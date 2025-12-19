package agent

func (s *LiveService) PlanScheduler() *PlanScheduler {
	if s == nil {
		return nil
	}
	return s.planScheduler
}

func (s *LiveService) Close() {
	if s == nil {
		return
	}
	if s.monitor != nil {
		s.monitor.Close()
	}
	if s.decLogs != nil {
		_ = s.decLogs.Close()
	}
	if s.strategyCloser != nil {
		_ = s.strategyCloser.Close()
	}
}
