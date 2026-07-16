package app

import (
	"context"
	"errors"

	"go.mau.fi/whatsmeow/types"
)

var errTooManyCalls = errors.New("max concurrent calls")

type StartedCall struct{ CallID, Peer, PeerName, PeerPhotoURL string }

func (s *Session) ID() string { return s.id }

func (s *Session) IsPaired() bool { return s.client.Store.ID != nil }

func (s *Session) HasCall(callID string) bool {
	_, ok := s.calls.Get(callID)
	return ok
}

func (s *Session) StartCall(ctx context.Context, phone string) (StartedCall, error) {
	if max := s.mgr.maxCalls; max > 0 && s.calls.Count() >= max {
		return StartedCall{}, errTooManyCalls
	}
	peer := types.NewJID(phone, types.DefaultUserServer)
	callID, err := s.calls.StartCall(ctx, peer)
	if err != nil {
		return StartedCall{}, err
	}
	return StartedCall{
		CallID:       callID,
		Peer:         peer.String(),
		PeerName:     resolvePeerName(ctx, s.client, peer),
		PeerPhotoURL: cachedPhotoURL(ctx, s.mgr.photos, s.id, peer.String()),
	}, nil
}

func (s *Session) FetchCallPhoto(callID, peer string) {
	jid, err := types.ParseJID(peer)
	if err != nil {
		return
	}
	go s.fetchPeerPhoto(jid, callID)
}
