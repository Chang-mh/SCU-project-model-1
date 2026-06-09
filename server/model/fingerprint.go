package model

import "time"

type FileFingerprint struct {
	SampleID   string `gorm:"primaryKey;size:64" json:"sensitive_file_id"`
	SHA256     string `gorm:"size:64;index" json:"sha256"`
	SimHash    string `gorm:"size:32" json:"simhash"`
	TextLength int    `json:"text_length"`
}

type SemanticFeature struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	SampleID       string    `gorm:"size:64;index" json:"sample_id"`
	SemanticLabels string    `gorm:"type:text" json:"semantic_labels"`
	EmbeddingID    string    `gorm:"size:128" json:"embedding_id"`
	Embedding      string    `gorm:"type:json" json:"embedding"`
	ModelName      string    `gorm:"size:128" json:"model_name"`
	CreatedAt      time.Time `json:"created_at"`
}
