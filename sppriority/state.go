package sppriority

type scheduleState struct {
	NodeCount map[string]int `json:"node_count"`
	Last      string         `json:"last"`
}

func (s *scheduleState) getScheduleCount(nodename string) int {
	if s.NodeCount == nil {
		return 0
	}
	if c, isOK := s.NodeCount[nodename]; isOK {
		return c
	}
	return 0

}

func (s *scheduleState) totalExceptLast() int {

	var total int
	for nodename, c := range s.NodeCount {
		if nodename == s.Last {
			continue
		}
		total = total + c

	}
	return total

}
