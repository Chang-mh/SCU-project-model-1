package dal

import (
	"scu-project-model-1/server/core"
	"scu-project-model-1/server/model"

	"go.uber.org/zap"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func InitDB(dsn string) error {
	var err error
	DB, err = gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return err
	}
	err = DB.AutoMigrate(
		&model.SensitiveSample{},
		&model.GeneratedRule{},
		&model.FileFingerprint{},
		&model.SemanticFeature{},
		&model.RuleVersion{},
		&model.ClientScanReport{},
		&model.ClientScanResult{},
	)
	if err != nil {
		return err
	}
	if err := core.SeedBuiltinRules(DB); err != nil {
		return err
	}
	zap.L().Info("Database migrated successfully")
	return nil
}
