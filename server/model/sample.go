package model

import "time"

type SensitiveSample struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	FileName      string    `gorm:"size:255" json:"file_name"`
	FileType      string    `gorm:"size:32" json:"file_type"`
	SensitiveType string    `gorm:"size:128" json:"sensitive_type"`
	RiskLevel     string    `gorm:"size:32" json:"risk_level"`
	SHA256        string    `gorm:"size:64;index" json:"sha256"`
	Explanation   string    `gorm:"type:text" json:"explanation"`
	ExtractedText string    `gorm:"type:longtext" json:"extracted_text"`
	UploadedAt    time.Time `json:"uploaded_at"`
}
