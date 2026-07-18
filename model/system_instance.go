package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SystemInstanceStatusOnline = "online"
	SystemInstanceStatusStale  = "stale"

	SystemInstanceStaleAfterSeconds int64 = 90
	// Instances not seen for this long are treated as permanently gone (a replaced
	// container / decommissioned node) and are pruned so stale rows don't pile up.
	SystemInstanceRetainSeconds int64 = 600
)

// PruneStaleSystemInstances deletes instances whose last heartbeat is older than
// SystemInstanceRetainSeconds. New deploys reuse the same NODE_NAME row, so this
// only removes genuinely-gone nodes (e.g. old container-id rows before NODE_NAME).
func PruneStaleSystemInstances() error {
	cutoff := common.GetTimestamp() - SystemInstanceRetainSeconds
	return DB.Where("last_seen_at < ?", cutoff).Delete(&SystemInstance{}).Error
}

type SystemInstance struct {
	NodeName   string `json:"node_name" gorm:"type:varchar(128);primaryKey"`
	Info       string `json:"info" gorm:"type:text"`
	StartedAt  int64  `json:"started_at" gorm:"bigint;index"`
	LastSeenAt int64  `json:"last_seen_at" gorm:"bigint;index"`
	CreatedAt  int64  `json:"created_at" gorm:"bigint;index"`
	UpdatedAt  int64  `json:"updated_at" gorm:"bigint;index"`
}

type SystemInstanceResponse struct {
	NodeName          string `json:"node_name"`
	Status            string `json:"status"`
	StaleAfterSeconds int64  `json:"stale_after_seconds"`
	StartedAt         int64  `json:"started_at"`
	LastSeenAt        int64  `json:"last_seen_at"`
	Info              any    `json:"info"`
}

func (instance *SystemInstance) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if instance.CreatedAt == 0 {
		instance.CreatedAt = now
	}
	if instance.UpdatedAt == 0 {
		instance.UpdatedAt = now
	}
	return nil
}

func UpsertSystemInstance(nodeName string, info any, startedAt int64, lastSeenAt int64) error {
	infoText, err := marshalSystemInstanceInfo(info)
	if err != nil {
		return err
	}
	if lastSeenAt == 0 {
		lastSeenAt = common.GetTimestamp()
	}
	instance := &SystemInstance{
		NodeName:   nodeName,
		Info:       infoText,
		StartedAt:  startedAt,
		LastSeenAt: lastSeenAt,
		UpdatedAt:  lastSeenAt,
	}
	return DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "node_name"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"info",
			"started_at",
			"last_seen_at",
			"updated_at",
		}),
	}).Create(instance).Error
}

func ListSystemInstances() ([]*SystemInstance, error) {
	// Prune-on-read: drop genuinely-gone nodes so the panel doesn't accumulate stale rows.
	_ = PruneStaleSystemInstances()
	var instances []*SystemInstance
	err := DB.Order("last_seen_at desc").Find(&instances).Error
	return instances, err
}

func (instance *SystemInstance) ToResponse(now int64) SystemInstanceResponse {
	status := SystemInstanceStatusOnline
	if now-instance.LastSeenAt > SystemInstanceStaleAfterSeconds {
		status = SystemInstanceStatusStale
	}
	return SystemInstanceResponse{
		NodeName:          instance.NodeName,
		Status:            status,
		StaleAfterSeconds: SystemInstanceStaleAfterSeconds,
		StartedAt:         instance.StartedAt,
		LastSeenAt:        instance.LastSeenAt,
		Info:              decodeSystemInstanceInfo(instance.Info),
	}
}

func marshalSystemInstanceInfo(v any) (string, error) {
	if v == nil {
		return "", nil
	}
	data, err := common.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeSystemInstanceInfo(data string) any {
	if data == "" {
		return nil
	}
	var value any
	if err := common.UnmarshalJsonStr(data, &value); err != nil {
		return data
	}
	return value
}
