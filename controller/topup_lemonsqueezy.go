package controller

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// Lemon Squeezy 一次性充值(topup)。对照 Creem:下单建 pending 单 → 调 LS API 拿 checkout URL
// → 用户支付 → webhook 验签(X-Signature = hex HMAC-SHA256(rawBody, secret))→ order_created/paid 入账。

const LemonSqueezySignatureHeader = "X-Signature"
const LemonSqueezyEventNameHeader = "X-Event-Name"
const lemonSqueezyCheckoutURL = "https://api.lemonsqueezy.com/v1/checkouts"

var lemonSqueezyAdaptor = &LemonSqueezyAdaptor{}

func verifyLemonSqueezySignature(payload string, signature string, secret string) bool {
	if secret == "" {
		if setting.LemonSqueezyTestMode {
			logger.LogInfo(context.Background(), "LemonSqueezy webhook 验签已跳过 reason=test_mode")
			return true
		}
		return false
	}
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(payload))
	expected := hex.EncodeToString(h.Sum(nil))
	return hmac.Equal([]byte(signature), []byte(expected))
}

type LemonSqueezyPayRequest struct {
	ProductId     string `json:"product_id"` // = LS variant_id
	PaymentMethod string `json:"payment_method"`
}

type LemonSqueezyProduct struct {
	ProductId string  `json:"productId"` // = LS variant_id
	Name      string  `json:"name"`
	Price     float64 `json:"price"`
	Currency  string  `json:"currency"`
	Quota     int64   `json:"quota"`
}

type LemonSqueezyAdaptor struct{}

func (*LemonSqueezyAdaptor) RequestPay(c *gin.Context, req *LemonSqueezyPayRequest) {
	if req.PaymentMethod != model.PaymentMethodLemonSqueezy {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "不支持的支付渠道"})
		return
	}
	if req.ProductId == "" {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "请选择产品"})
		return
	}

	var products []LemonSqueezyProduct
	if err := json.Unmarshal([]byte(setting.LemonSqueezyProducts), &products); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("LemonSqueezy 产品配置解析失败 user_id=%d error=%q", c.GetInt("id"), err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "产品配置错误"})
		return
	}

	var selected *LemonSqueezyProduct
	for i := range products {
		if products[i].ProductId == req.ProductId {
			selected = &products[i]
			break
		}
	}
	if selected == nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "产品不存在"})
		return
	}

	id := c.GetInt("id")
	user, _ := model.GetUserById(id, false)

	reference := fmt.Sprintf("ls-api-ref-%d-%d-%s", user.Id, time.Now().UnixMilli(), randstr.String(4))
	referenceId := "ref_" + common.Sha1([]byte(reference))

	topUp := &model.TopUp{
		UserId:          id,
		Amount:          selected.Quota,
		Money:           selected.Price,
		TradeNo:         referenceId,
		PaymentMethod:   model.PaymentMethodLemonSqueezy,
		PaymentProvider: model.PaymentProviderLemonSqueezy,
		CreateTime:      time.Now().Unix(),
		Status:          common.TopUpStatusPending,
	}
	if err := topUp.Insert(); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("LemonSqueezy 创建充值订单失败 user_id=%d trade_no=%s error=%q", id, referenceId, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "创建订单失败"})
		return
	}

	checkoutURL, err := genLemonSqueezyLink(c.Request.Context(), referenceId, selected, user.Email)
	if err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("LemonSqueezy 创建支付链接失败 user_id=%d trade_no=%s error=%q", id, referenceId, err.Error()))
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "拉起支付失败"})
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("LemonSqueezy 充值订单创建成功 user_id=%d trade_no=%s variant=%s quota=%d money=%.2f", id, referenceId, selected.ProductId, selected.Quota, selected.Price))

	c.JSON(http.StatusOK, gin.H{
		"message": "success",
		"data": gin.H{
			"checkout_url": checkoutURL,
			"order_id":     referenceId,
		},
	})
}

func RequestLemonSqueezyPay(c *gin.Context) {
	var req LemonSqueezyPayRequest
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "read query error"})
		return
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusOK, gin.H{"message": "error", "data": "参数错误"})
		return
	}
	lemonSqueezyAdaptor.RequestPay(c, &req)
}

// ── checkout 创建(LS v1 JSON:API)──

type lsRelInner struct {
	Type string `json:"type"`
	Id   string `json:"id"`
}
type lsRel struct {
	Data lsRelInner `json:"data"`
}
type lsCheckoutData struct {
	Email  string            `json:"email,omitempty"`
	Custom map[string]string `json:"custom,omitempty"`
}
type lsCheckoutReq struct {
	Data struct {
		Type       string `json:"type"`
		Attributes struct {
			CheckoutData lsCheckoutData `json:"checkout_data"`
		} `json:"attributes"`
		Relationships struct {
			Store   lsRel `json:"store"`
			Variant lsRel `json:"variant"`
		} `json:"relationships"`
	} `json:"data"`
}
type lsCheckoutResp struct {
	Data struct {
		Attributes struct {
			Url string `json:"url"`
		} `json:"attributes"`
	} `json:"data"`
}

func genLemonSqueezyLink(ctx context.Context, referenceId string, product *LemonSqueezyProduct, email string) (string, error) {
	if setting.LemonSqueezyApiKey == "" {
		return "", fmt.Errorf("未配置 Lemon Squeezy API 密钥")
	}
	if setting.LemonSqueezyStoreId == "" {
		return "", fmt.Errorf("未配置 Lemon Squeezy Store ID")
	}

	var reqBody lsCheckoutReq
	reqBody.Data.Type = "checkouts"
	reqBody.Data.Attributes.CheckoutData.Email = email
	reqBody.Data.Attributes.CheckoutData.Custom = map[string]string{"reference_id": referenceId}
	reqBody.Data.Relationships.Store.Data = lsRelInner{Type: "stores", Id: setting.LemonSqueezyStoreId}
	reqBody.Data.Relationships.Variant.Data = lsRelInner{Type: "variants", Id: product.ProductId}

	jsonData, err := json.Marshal(&reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求数据失败: %v", err)
	}

	httpReq, err := http.NewRequest("POST", lemonSqueezyCheckoutURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建HTTP请求失败: %v", err)
	}
	httpReq.Header.Set("Accept", "application/vnd.api+json")
	httpReq.Header.Set("Content-Type", "application/vnd.api+json")
	httpReq.Header.Set("Authorization", "Bearer "+setting.LemonSqueezyApiKey)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("发送HTTP请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	if resp.StatusCode/100 != 2 {
		logger.LogError(ctx, fmt.Sprintf("LemonSqueezy API 非2xx trade_no=%s status=%d body=%q", referenceId, resp.StatusCode, string(body)))
		return "", fmt.Errorf("Lemon Squeezy API http status %d", resp.StatusCode)
	}

	var checkoutResp lsCheckoutResp
	if err := json.Unmarshal(body, &checkoutResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}
	if checkoutResp.Data.Attributes.Url == "" {
		return "", fmt.Errorf("Lemon Squeezy API resp no checkout url")
	}

	logger.LogInfo(ctx, fmt.Sprintf("LemonSqueezy 支付链接创建成功 trade_no=%s", referenceId))
	return checkoutResp.Data.Attributes.Url, nil
}

// ── webhook ──

type lemonSqueezyWebhookEvent struct {
	Meta struct {
		EventName  string            `json:"event_name"`
		CustomData map[string]string `json:"custom_data"`
	} `json:"meta"`
	Data struct {
		Id         string `json:"id"`
		Attributes struct {
			Status    string `json:"status"`
			UserEmail string `json:"user_email"`
			UserName  string `json:"user_name"`
			Total     int    `json:"total"`
			Currency  string `json:"currency"`
		} `json:"attributes"`
	} `json:"data"`
}

func LemonSqueezyWebhook(c *gin.Context) {
	if !isLemonSqueezyWebhookEnabled() {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LemonSqueezy webhook 被拒绝 reason=webhook_disabled client_ip=%s", c.ClientIP()))
		c.AbortWithStatus(http.StatusForbidden)
		return
	}

	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	signature := c.GetHeader(LemonSqueezySignatureHeader)
	eventName := c.GetHeader(LemonSqueezyEventNameHeader)
	logger.LogInfo(c.Request.Context(), fmt.Sprintf("LemonSqueezy webhook 收到 event=%s client_ip=%s", eventName, c.ClientIP()))

	if signature == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LemonSqueezy webhook 缺少签名 client_ip=%s", c.ClientIP()))
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	if !verifyLemonSqueezySignature(string(bodyBytes), signature, setting.LemonSqueezyWebhookSecret) {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LemonSqueezy webhook 验签失败 client_ip=%s", c.ClientIP()))
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}

	var event lemonSqueezyWebhookEvent
	if err := json.Unmarshal(bodyBytes, &event); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("LemonSqueezy webhook 解析失败 error=%q body=%q", err.Error(), string(bodyBytes)))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	if eventName == "" {
		eventName = event.Meta.EventName
	}

	// 只处理一次性订单支付完成
	if eventName != "order_created" {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("LemonSqueezy webhook 忽略事件 event=%s", eventName))
		c.Status(http.StatusOK)
		return
	}
	if event.Data.Attributes.Status != "paid" {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订单未支付,忽略 status=%s order_id=%s", event.Data.Attributes.Status, event.Data.Id))
		c.Status(http.StatusOK)
		return
	}

	referenceId := event.Meta.CustomData["reference_id"]
	if referenceId == "" {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LemonSqueezy webhook 缺少 reference_id order_id=%s", event.Data.Id))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}

	LockOrder(referenceId)
	defer UnlockOrder(referenceId)

	topUp := model.GetTopUpByTradeNo(referenceId)
	if topUp == nil {
		logger.LogWarn(c.Request.Context(), fmt.Sprintf("LemonSqueezy 充值订单不存在 trade_no=%s order_id=%s", referenceId, event.Data.Id))
		c.AbortWithStatus(http.StatusBadRequest)
		return
	}
	if topUp.Status != common.TopUpStatusPending {
		logger.LogInfo(c.Request.Context(), fmt.Sprintf("LemonSqueezy 订单非 pending,忽略 trade_no=%s status=%s", referenceId, topUp.Status))
		c.Status(http.StatusOK)
		return
	}

	if err := model.RechargeLemonSqueezy(referenceId, event.Data.Attributes.UserEmail, event.Data.Attributes.UserName, c.ClientIP()); err != nil {
		logger.LogError(c.Request.Context(), fmt.Sprintf("LemonSqueezy 充值处理失败 trade_no=%s error=%q", referenceId, err.Error()))
		c.AbortWithStatus(http.StatusInternalServerError)
		return
	}

	logger.LogInfo(c.Request.Context(), fmt.Sprintf("LemonSqueezy 充值成功 trade_no=%s order_id=%s quota=%d money=%.2f", referenceId, event.Data.Id, topUp.Amount, topUp.Money))
	c.Status(http.StatusOK)
}
