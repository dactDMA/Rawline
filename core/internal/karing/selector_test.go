package karing

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/common/urltest"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/group"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/service"
)

type fixtureOutbound struct{ outbound.Adapter }
type fixtureManager struct {
	adapter.OutboundManager
	members map[string]adapter.Outbound
}

func (m fixtureManager) Outbound(tag string) (adapter.Outbound, bool) {
	member, ok := m.members[tag]
	return member, ok
}

func (*fixtureOutbound) DialContext(context.Context, string, M.Socksaddr) (net.Conn, error) {
	panic("not dialed")
}
func (*fixtureOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	panic("not dialed")
}

func TestKaringURLTestSelection(t *testing.T) {
	history := urltest.NewHistoryStorage()
	ctx := service.ContextWithPtr(context.Background(), history)
	first := &fixtureOutbound{outbound.NewAdapter("direct", "first", []string{N.NetworkTCP}, nil)}
	second := &fixtureOutbound{outbound.NewAdapter("direct", "second", []string{N.NetworkTCP, N.NetworkUDP}, nil)}
	store := func(tag string, delay uint16) {
		history.StoreURLTestHistory(tag, &adapter.URLTestHistory{Time: time.Now(), Delay: delay})
	}
	g, err := group.NewURLTestGroup(ctx, nil, nil, []adapter.Outbound{first, second}, "", time.Minute, 50, time.Hour, false)
	if err != nil {
		t.Fatal(err)
	}
	check := func(network, want string, measured bool) {
		t.Helper()
		got, exists := g.Select(network)
		if got == nil || got.Tag() != want || exists != measured {
			t.Fatalf("%s: got %v/%v, want %s/%v", network, got, exists, want, measured)
		}
	}
	check(N.NetworkTCP, "first", false)
	check(N.NetworkUDP, "second", false)
	store("first", 200)
	store("second", 170)
	check(N.NetworkTCP, "first", true) // The 50 ms tolerance prevents churn.
	store("second", 149)
	check(N.NetworkTCP, "second", true)
	store("first", 80) // Latest successful result, without a sampling average.
	check(N.NetworkTCP, "first", true)
	history.DeleteURLTestHistory("first")
	check(N.NetworkTCP, "second", true)
	history.DeleteURLTestHistory("second")
	check(N.NetworkTCP, "first", false) // All failed: a usable network member remains a fallback.
}

func TestIncumbentToleranceAndManualSelection(t *testing.T) {
	history := urltest.NewHistoryStorage()
	first := &fixtureOutbound{outbound.NewAdapter("direct", "first", []string{N.NetworkTCP}, nil)}
	second := &fixtureOutbound{outbound.NewAdapter("direct", "second", []string{N.NetworkTCP, N.NetworkUDP}, nil)}
	ctx := service.ContextWithPtr(context.Background(), history)
	ctx = service.ContextWith[adapter.OutboundManager](ctx, fixtureManager{members: map[string]adapter.Outbound{"first": first, "second": second}})
	created, err := New(ctx, nil, nil, "test", option.AutoSelectorOutboundOptions{Outbounds: []string{"first", "second"}, Tolerance: 50, Pinned: "outside-the-group"})
	if err != nil {
		t.Fatal(err)
	}
	s := created.(*Selector)
	if err = s.Start(adapter.StartStateStart, nil); err != nil {
		t.Fatal(err)
	}
	store := func(tag string, delay uint16) {
		history.StoreURLTestHistory(tag, &adapter.URLTestHistory{Time: time.Now(), Delay: delay})
	}
	check := func(want string) {
		t.Helper()
		s.PerformUpdateCheck()
		if got := s.Selected(N.NetworkTCP).Tag(); got != want {
			t.Fatalf("got %s, want %s", got, want)
		}
	}
	store("first", 200)
	store("second", 100)
	check("second")
	store("first", 60)
	check("second")
	store("first", 49)
	check("first")
	if s.SelectOutbound("outside-the-group") || !s.SelectOutbound("second") {
		t.Fatal("manual membership check")
	}
	check("second")
	if !s.SelectOutbound("") {
		t.Fatal("return to automatic selection")
	}
	check("first")
	history.DeleteURLTestHistory("first")
	check("second")
	if status := s.Status(); status.MembersAlive != 1 || status.Selected != "second" || status.SelectedUDP != "second" {
		t.Fatalf("status: %+v", status)
	}
}
