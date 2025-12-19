package sqlite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"brale/internal/store"
	"brale/internal/store/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type SqliteStore struct {
	db *gorm.DB
}

func NewSqliteStore(path string) (*SqliteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("database path cannot be empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&cache=shared", path)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:                                   logger.Default.LogMode(logger.Silent),
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		return nil, err
	}

	return newSqliteStore(db)
}

func NewSqliteStoreFromDB(db *gorm.DB) (*SqliteStore, error) {
	if db == nil {
		return nil, fmt.Errorf("gorm db 不能为空")
	}
	return newSqliteStore(db)
}

func newSqliteStore(db *gorm.DB) (*SqliteStore, error) {
	models := []interface{}{
		&model.LiveOrderModel{},
		&model.StrategyInstanceModel{},
		&model.TradeOperationModel{},
	}
	if err := db.AutoMigrate(models...); err != nil {
		return nil, err
	}
	if sqlDB, err := db.DB(); err == nil {

		sqlDB.SetMaxOpenConns(2)
		sqlDB.SetMaxIdleConns(2)
	}
	return &SqliteStore{db: db}, nil
}

func (s *SqliteStore) Begin(ctx context.Context) (store.UnitOfWork, error) {
	tx := s.db.WithContext(ctx).Begin()
	if tx.Error != nil {
		return nil, tx.Error
	}
	return &gormUnitOfWork{tx: tx}, nil
}

func (s *SqliteStore) Close() error {
	if s.db == nil {
		return nil
	}
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

type gormUnitOfWork struct {
	tx *gorm.DB
}

func (u *gormUnitOfWork) Orders() store.OrderRepository {
	return NewOrderRepo(u.tx)
}

func (u *gormUnitOfWork) Strategies() store.StrategyRepository {
	return NewStrategyRepo(u.tx)
}

func (u *gormUnitOfWork) Logs() store.LogRepository {
	return NewLogRepo(u.tx)
}

func (u *gormUnitOfWork) Commit() error {
	return u.tx.Commit().Error
}

func (u *gormUnitOfWork) Rollback() error {
	return u.tx.Rollback().Error
}
