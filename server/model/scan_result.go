package model

import "time"

type ClientScanReport struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	HostID      string    `gorm:"size:128;index" json:"host_id"`
	ScanPath    string    `gorm:"type:text" json:"scan_path"`
	ScannedAt   string    `gorm:"size:64" json:"scanned_at"`
	ResultCount int       `json:"result_count"`
	CreatedAt   time.Time `json:"created_at"`
}

type ClientScanResult struct {
	ID              uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ReportID        string    `gorm:"size:64;index" json:"report_id"`
	FilePath        string    `gorm:"type:text" json:"file_path"`
	FileHash        string    `gorm:"size:64;index" json:"file_hash"`
	Sensitive       bool      `gorm:"index" json:"sensitive"`
	ConfidenceLevel string    `gorm:"size:32;index" json:"confidence_level"`
	MatchScore      int       `gorm:"index" json:"match_score"`
	RiskLevel       string    `gorm:"size:32;index" json:"risk_level"`
	SensitiveType   string    `gorm:"size:128;index" json:"sensitive_type"`
	SensitiveFileID string    `gorm:"size:64;index" json:"sensitive_file_id"`
	MatchDetail     string    `gorm:"type:json" json:"match_detail"`
	CreatedAt       time.Time `json:"created_at"`
}
