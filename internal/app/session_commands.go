package app

func (s *Session) ID() string { return s.id }

func (s *Session) IsPaired() bool { return s.client.Store.ID != nil }
