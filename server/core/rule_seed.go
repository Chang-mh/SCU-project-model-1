package core

import (
	"fmt"
	"time"

	"scu-project-model-1/server/model"

	"gorm.io/gorm"
)

const BuiltinRuleSource = "builtin"

func BuiltinRuleID(name string) string {
	return "builtin_" + name
}

func SeedBuiltinRules(db *gorm.DB) error {
	now := time.Now()
	for _, item := range BuiltinRegexRules {
		rule := model.GeneratedRule{
			ID:            BuiltinRuleID(item.Name),
			Version:       0,
			RuleType:      "regex",
			SensitiveType: "固定格式敏感信息",
			RiskLevel:     item.RiskLevel,
			Content: RuleContentJSON(map[string]any{
				"name":    item.Name,
				"pattern": item.Pattern,
			}),
			Source:    BuiltinRuleSource,
			Enabled:   true,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := db.Where("id = ?", rule.ID).FirstOrCreate(&rule).Error; err != nil {
			return fmt.Errorf("seed builtin rule %s: %w", item.Name, err)
		}
	}
	return nil
}
