package endpoints

import "sync"

// sync4xx counts responses that were not 2xx, separately from the driver's
// error tally.
//
// The two differ in a way worth keeping: the driver counts anything that came
// back as an error, including a transport failure with no status at all, while
// this counts requests the server did answer and refused. A run that is
// silently 401ing every request otherwise looks like a very fast API.
type sync4xx struct {
	mu     sync.Mutex
	counts map[int]int
}

func (s *sync4xx) record(status int) {
	if status < 400 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.counts == nil {
		s.counts = map[int]int{}
	}
	s.counts[status]++
}

func (s *sync4xx) bad() map[int]int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.counts) == 0 {
		return nil
	}

	out := make(map[int]int, len(s.counts))
	for k, v := range s.counts {
		out[k] = v
	}
	return out
}
