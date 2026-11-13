package sentinel

// TakeNudgeForTest reports whether a nudge is pending and clears it.
func (c *Controller) TakeNudgeForTest() bool {
	select {
	case <-c.nudge:
		return true
	default:
		return false
	}
}
