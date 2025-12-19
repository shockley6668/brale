package agent

import "brale/internal/logger"

func (s *LiveService) PlanScheduler() *PlanScheduler {
	if s == nil {
		return nil
	}
	return s.planScheduler
}

func (s *LiveService) Close() error {
	if s == nil {
		return nil
	}
	logger.Infof("LiveService: 开始优雅关闭...")

	if s.monitor != nil {
		s.monitor.Close()
		logger.Infof("LiveService: ✓ PriceMonitor 已关闭")
	}
	if s.decLogs != nil {
		if err := s.decLogs.Close(); err != nil {
			logger.Warnf("LiveService: DecisionLogStore 关闭失败: %v", err)
		} else {
			logger.Infof("LiveService: ✓ DecisionLogStore 已关闭")
		}
	}
	if s.strategyCloser != nil {
		if err := s.strategyCloser.Close(); err != nil {
			logger.Warnf("LiveService: StrategyStore 关闭失败: %v", err)
		} else {
			logger.Infof("LiveService: ✓ StrategyStore(GormStore) 已关闭，WAL 已刷新")
		}
	}

	logger.Infof("LiveService: ✓ 优雅关闭完成")
	return nil
}
