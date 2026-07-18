package controller

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/thanhpk/randstr"
)

// Lemon Squeezy 订阅(包月/自动续费)。对照 Creem 订阅(subscription_payment_creem.go):
// 下单建 pending SubscriptionOrder → 调 LS API 拿订阅型 variant 的 checkout URL →
// 用户支付 → webhook 按事件分发(见 topup_lemonsqueezy.go 的 LemonSqueezyWebhook):
//   subscription_created           → 首次激活+发放(CompleteSubscriptionOrder,幂等)
//   subscription_payment_success   → 续费再发放(RenewSubscriptionForLemonSqueezy,按发票 id 幂等)
//   subscription_cancelled         → 关闭自动续费,当前周期结束前仍有效(仅记录)
//   subscription_expired           → 立即到期+分组回退(ExpireUserSubscriptionForLemonSqueezy)

type SubscriptionLemonSqueezyPayRequest struct {
	PlanId int `json:"plan_id"`
}

func SubscriptionRequestLemonSqueezyPay(c *gin.Context) {
	if !requirePaymentCompliance(c) {
		return
	}

	var req SubscriptionLemonSqueezyPayRequest

	// 保留 body 便于排错(对照 SubscriptionRequestCreemPay)
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅支付请求读取失败 error=%q", err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "read query error"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	if err := c.ShouldBindJSON(&req); err != nil || req.PlanId <= 0 {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}

	plan, err := model.GetSubscriptionPlanById(req.PlanId)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !plan.Enabled {
		common.ApiErrorMsg(c, "套餐未启用")
		return
	}
	if plan.LemonSqueezyVariantId == "" {
		common.ApiErrorMsg(c, "该套餐未配置 LemonSqueezyVariantId")
		return
	}
	if setting.LemonSqueezyApiKey == "" || setting.LemonSqueezyStoreId == "" {
		common.ApiErrorMsg(c, "Lemon Squeezy 未配置")
		return
	}
	if setting.LemonSqueezyWebhookSecret == "" && !setting.LemonSqueezyTestMode {
		common.ApiErrorMsg(c, "Lemon Squeezy Webhook 未配置")
		return
	}

	userId := c.GetInt("id")
	user, err := model.GetUserById(userId, false)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if user == nil {
		common.ApiErrorMsg(c, "用户不存在")
		return
	}

	if plan.MaxPurchasePerUser > 0 {
		count, err := model.CountUserSubscriptionsByPlan(userId, plan.Id)
		if err != nil {
			common.ApiError(c, err)
			return
		}
		if count >= int64(plan.MaxPurchasePerUser) {
			common.ApiErrorMsg(c, "已达到该套餐购买上限")
			return
		}
	}

	reference := fmt.Sprintf("sub-ls-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "sub_ref_" + common.Sha1([]byte(reference))

	// 先建 pending 订单
	order := &model.SubscriptionOrder{
		UserId:          userId,
		PlanId:          plan.Id,
		Money:           plan.PriceAmount,
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodLemonSqueezy,
		PaymentProvider: model.PaymentProviderLemonSqueezy,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := order.Insert(); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	// 复用 topup 的 checkout 生成器:custom.reference_id = referenceId,
	// LS 会在后续 subscription_* 事件的 meta.custom_data 中回传。
	product := &LemonSqueezyProduct{
		ProductId: plan.LemonSqueezyVariantId,
		Name:      plan.Title,
		Price:     plan.PriceAmount,
		Currency:  "USD",
		Quota:     0,
	}

	checkoutURL, err := genLemonSqueezyLink(c.Request.Context(), referenceId, product, user.Email)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅支付链接创建失败 trade_no=%s variant=%s error=%q", referenceId, product.ProductId, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"checkout_url": checkoutURL,
			"order_id":     referenceId,
		},
	})
}

// handleLemonSqueezySubscriptionActivated 处理 subscription_created 与 subscription_payment_success。
// 首期(created 或 payment_success[initial])→ CompleteSubscriptionOrder(幂等,创建 UserSubscription、
// 发放额度、按 plan 升级分组);续费(payment_success[renewal/updated])→ RenewSubscriptionForLemonSqueezy。
func handleLemonSqueezySubscriptionActivated(c *gin.Context, event *lemonSqueezyWebhookEvent, eventName string) {
	referenceId := event.Meta.CustomData["reference_id"]
	if referenceId == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅事件缺少 reference_id event=%s data_id=%s", eventName, event.Data.Id))
		c.Status(http.StatusOK) // 无法映射到本地订单,直接 ack 避免无意义重试
		return
	}

	billingReason := event.Data.Attributes.BillingReason
	isActivation := eventName == "subscription_created" ||
		(eventName == "subscription_payment_success" && billingReason == "initial")

	if isActivation {
		LockOrder(referenceId)
		defer UnlockOrder(referenceId)
		if err := model.CompleteSubscriptionOrder(referenceId, common.GetJsonString(event), model.PaymentProviderLemonSqueezy, ""); err != nil {
			if errors.Is(err, model.ErrSubscriptionOrderNotFound) {
				logger.LogWarn(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅订单不存在 trade_no=%s event=%s data_id=%s", referenceId, eventName, event.Data.Id))
				c.Status(http.StatusOK)
				return
			}
			logger.LogError(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅激活失败 trade_no=%s event=%s data_id=%s error=%q", referenceId, eventName, event.Data.Id, err.Error()))
			c.AbortWithStatus(http.StatusInternalServerError)
			return
		}
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅激活成功 trade_no=%s event=%s data_id=%s", referenceId, eventName, event.Data.Id))
		c.Status(http.StatusOK)
		return
	}

	// 续费:以发票 id 作为幂等键
	renewalTradeNo := "ls_sub_renew_" + event.Data.Id
	LockOrder(renewalTradeNo)
	defer UnlockOrder(renewalTradeNo)
	if err := model.RenewSubscriptionForLemonSqueezy(referenceId, renewalTradeNo, common.GetJsonString(event), model.PaymentProviderLemonSqueezy); err != nil {
		if errors.Is(err, model.ErrSubscriptionOrderNotFound) {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("LemonSqueezy 续费原始订单不存在 trade_no=%s invoice_id=%s", referenceId, event.Data.Id))
			c.Status(http.StatusOK)
			return
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅续费失败 trade_no=%s invoice_id=%s error=%q", referenceId, event.Data.Id, err.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅续费成功 trade_no=%s invoice_id=%s billing_reason=%s", referenceId, event.Data.Id, billingReason))
	c.Status(http.StatusOK)
}

// handleLemonSqueezySubscriptionCancelled 处理 subscription_cancelled。
// LS 语义:cancelled = 关闭自动续费,当前计费周期结束前订阅仍然有效。真正的到期由
// subscription_expired 事件(或 ExpireDueSubscriptions 定时任务按 end_time)处理,故此处仅记录。
func handleLemonSqueezySubscriptionCancelled(c *gin.Context, event *lemonSqueezyWebhookEvent) {
	referenceId := event.Meta.CustomData["reference_id"]
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅已关闭自动续费,周期结束前仍有效 trade_no=%s status=%s data_id=%s", referenceId, event.Data.Attributes.Status, event.Data.Id))
	c.Status(http.StatusOK)
}

// handleLemonSqueezySubscriptionExpired 处理 subscription_expired:立即结束订阅并回退用户分组。
func handleLemonSqueezySubscriptionExpired(c *gin.Context, event *lemonSqueezyWebhookEvent) {
	referenceId := event.Meta.CustomData["reference_id"]
	if referenceId == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅到期事件缺少 reference_id data_id=%s", event.Data.Id))
		c.Status(http.StatusOK)
		return
	}

	LockOrder(referenceId)
	defer UnlockOrder(referenceId)
	downgradeGroup, userId, err := model.ExpireUserSubscriptionForLemonSqueezy(referenceId, model.PaymentProviderLemonSqueezy)
	if err != nil {
		if errors.Is(err, model.ErrSubscriptionOrderNotFound) {
			logger.LogWarn(c.Request.Context(), fmt.Sprintf("LemonSqueezy 到期事件对应订单不存在 trade_no=%s data_id=%s", referenceId, event.Data.Id))
			c.Status(http.StatusOK)
			return
		}
		logger.LogError(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅到期处理失败 trade_no=%s data_id=%s error=%q", referenceId, event.Data.Id, err.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订阅已到期 trade_no=%s data_id=%s user_id=%d downgrade_group=%q", referenceId, event.Data.Id, userId, downgradeGroup))
	c.Status(http.StatusOK)
}
