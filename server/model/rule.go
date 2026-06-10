package model

import "time"

type GeneratedRule struct {
	ID            string    `gorm:"primaryKey;size:64" json:"rule_id"`
	SampleID      string    `gorm:"size:64;index" json:"sample_id"`
	Version       int       `gorm:"index" json:"version"`
	RuleType      string    `gorm:"size:32" json:"rule_type"`
	SensitiveType string    `gorm:"size:128" json:"sensitive_type"`
	RiskLevel     string    `gorm:"size:32" json:"risk_level"`
	Content       string    `gorm:"type:json" json:"content"`
	CreatedAt     time.Time `json:"created_at"`
}

type RuleVersion struct {
	Version    int       `gorm:"primaryKey;autoIncrement" json:"version"`
	ChangeType string    `gorm:"size:32" json:"change_type"`
	CreatedAt  time.Time `json:"created_at"`
}
