package manualclock

import (
	"reflect"
	"testing"
	"time"
)

func TestTimerFiresAtDeadline(t *testing.T) {
	clock := New(time.Unix(100, 0))
	timer := clock.NewTimer(10 * time.Second)

	clock.Advance(9 * time.Second)
	select {
	case value := <-timer.C():
		t.Fatalf("TIME-CLOCK-001 timer fired early at %s", value)
	default:
	}

	clock.Advance(time.Second)
	select {
	case value := <-timer.C():
		if want := time.Unix(110, 0); !value.Equal(want) {
			t.Fatalf("TIME-CLOCK-001 fired at %s, want %s", value, want)
		}
	default:
		t.Fatal("TIME-CLOCK-001 timer did not fire at deadline")
	}
}

func TestCallbacksAtSameDeadlineKeepRegistrationOrder(t *testing.T) {
	clock := New(time.Unix(100, 0))
	order := make([]int, 0, 3)
	clock.AfterFunc(time.Second, func() { order = append(order, 1) })
	clock.AfterFunc(time.Second, func() { order = append(order, 2) })
	clock.AfterFunc(time.Second, func() { order = append(order, 3) })

	clock.Advance(time.Second)
	if want := []int{1, 2, 3}; !reflect.DeepEqual(order, want) {
		t.Fatalf("TIME-CLOCK-002 order = %v, want %v", order, want)
	}
}

func TestTimerStopResetAndReentrantSchedule(t *testing.T) {
	clock := New(time.Unix(100, 0))
	order := make([]string, 0, 2)
	timer := clock.AfterFunc(10*time.Second, func() {
		order = append(order, "outer")
		clock.AfterFunc(0, func() { order = append(order, "inner") })
	})

	if !timer.Stop() {
		t.Fatal("TIME-CLOCK-003 active timer Stop returned false")
	}
	clock.Advance(10 * time.Second)
	if len(order) != 0 {
		t.Fatalf("TIME-CLOCK-003 stopped timer fired: %v", order)
	}
	if timer.Reset(5 * time.Second) {
		t.Fatal("TIME-CLOCK-003 reset stopped timer reported active")
	}
	clock.Advance(5 * time.Second)
	if want := []string{"outer", "inner"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("TIME-CLOCK-003 order = %v, want %v", order, want)
	}
}

func TestTickerDropsUnreadTicksAndStops(t *testing.T) {
	clock := New(time.Unix(100, 0))
	ticker := clock.NewTicker(2 * time.Second)

	clock.Advance(2 * time.Second)
	if got := <-ticker.C(); !got.Equal(time.Unix(102, 0)) {
		t.Fatalf("TIME-CLOCK-004 first tick = %s", got)
	}

	clock.Advance(6 * time.Second)
	if got := <-ticker.C(); !got.Equal(time.Unix(104, 0)) {
		t.Fatalf("TIME-CLOCK-004 retained tick = %s, want first unread tick", got)
	}
	select {
	case got := <-ticker.C():
		t.Fatalf("TIME-CLOCK-004 ticker retained extra tick %s", got)
	default:
	}

	ticker.Stop()
	clock.Advance(2 * time.Second)
	select {
	case got := <-ticker.C():
		t.Fatalf("TIME-CLOCK-004 stopped ticker fired at %s", got)
	default:
	}
}

func TestAdvanceRejectsNegativeDuration(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("negative Advance did not panic")
		}
	}()
	New(time.Unix(100, 0)).Advance(-time.Second)
}
