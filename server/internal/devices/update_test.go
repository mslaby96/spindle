package devices

import (
	"context"
	"errors"
	"testing"
)

func standOn(t *testing.T, fw string) (*Store, int64, Device, *[]Device) {
	t.Helper()
	s, userA, _ := newStore(t)
	var notified []Device
	s.Notify = func(d Device) { notified = append(notified, d) }
	s.Firmware = "0.6.0"
	dev, _, _ := s.Create(context.Background(), userA, "Stand")
	if fw != "" {
		s.Touch(context.Background(), dev.ID, Report{FW: fw})
	}
	return s, userA, dev, &notified
}

func TestAStandOnOtherFirmwareCanUpdate(t *testing.T) {
	s, userA, _, _ := standOn(t, "0.5.0")
	list, _ := s.List(context.Background(), userA)
	if list[0].UpdateAvailable != "0.6.0" {
		t.Fatalf("on 0.5.0: %+v", list[0])
	}
	s.Firmware = "0.5.0"
	if list, _ := s.List(context.Background(), userA); list[0].UpdateAvailable != "" {
		t.Fatalf("on the bundled version: %+v", list[0])
	}
	s.Firmware = ""
	if list, _ := s.List(context.Background(), userA); list[0].UpdateAvailable != "" {
		t.Fatalf("nothing bundled: %+v", list[0])
	}
}

func TestAStandThatNeverReportedHasNothingToUpdate(t *testing.T) {
	s, userA, _, _ := standOn(t, "")
	if list, _ := s.List(context.Background(), userA); list[0].UpdateAvailable != "" {
		t.Fatalf("%+v", list[0])
	}
}

func TestUpdateGoesFromRequestedToInstalled(t *testing.T) {
	s, userA, dev, notified := standOn(t, "0.5.0")
	ctx := context.Background()
	if err := s.RequestUpdate(ctx, userA, dev.ID); err != nil {
		t.Fatal(err)
	}
	if got := (*notified)[0].Update; got == nil || *got != (Update{Version: "0.6.0", State: UpdateRequested}) {
		t.Fatalf("requested: %+v", got)
	}
	version, ok, err := s.TakeUpdate(ctx, dev.ID)
	if version != "0.6.0" || !ok || err != nil {
		t.Fatalf("take: %q %v %v", version, ok, err)
	}
	if _, ok, _ := s.TakeUpdate(ctx, dev.ID); ok {
		t.Fatal("handed over twice")
	}
	if got := (*notified)[1].Update; got.State != UpdateInstalling {
		t.Fatalf("installing: %+v", got)
	}
	s.Settle(ctx, dev.ID, "0.5.0", "")
	if len(*notified) != 2 {
		t.Fatalf("a report from the old version, still downloading, settled it: %+v", *notified)
	}
	s.Settle(ctx, dev.ID, "0.6.0", "")
	if got := (*notified)[2]; got.Update != nil || got.FWVersion != "0.5.0" {
		t.Fatalf("installed: %+v", got)
	}
}

func TestAFailedUpdateIsShownUntilDismissed(t *testing.T) {
	s, userA, dev, notified := standOn(t, "0.5.0")
	ctx := context.Background()
	s.RequestUpdate(ctx, userA, dev.ID)
	s.TakeUpdate(ctx, dev.ID)
	s.Settle(ctx, dev.ID, "0.5.0", "0.4.0")
	if len(*notified) != 2 {
		t.Fatal("an older failure counted for this update")
	}
	s.Settle(ctx, dev.ID, "0.5.0", "0.6.0")
	if got := (*notified)[2].Update; got == nil || *got != (Update{Version: "0.6.0", State: UpdateFailed}) {
		t.Fatalf("failed: %+v", got)
	}
	s.Settle(ctx, dev.ID, "0.5.0", "0.6.0")
	if len(*notified) != 3 {
		t.Fatal("the same failure announced again")
	}
	if err := s.CancelUpdate(ctx, userA, dev.ID); err != nil {
		t.Fatal(err)
	}
	if len(*notified) != 4 || (*notified)[3].Update != nil {
		t.Fatalf("dismissing was not announced: %+v", *notified)
	}
	if list, _ := s.List(ctx, userA); list[0].Update != nil {
		t.Fatalf("dismissed: %+v", list[0].Update)
	}
}

func TestAStandUpdatedSomeOtherWayClearsTheRequest(t *testing.T) {
	s, userA, dev, notified := standOn(t, "0.5.0")
	ctx := context.Background()
	s.RequestUpdate(ctx, userA, dev.ID)
	s.Settle(ctx, dev.ID, "0.6.0", "")
	if got := (*notified)[1]; got.Update != nil {
		t.Fatalf("%+v", got)
	}
}

func TestUpdateRequestsThatMakeNoSense(t *testing.T) {
	s, userA, dev, _ := standOn(t, "0.6.0")
	ctx := context.Background()
	if err := s.RequestUpdate(ctx, userA, dev.ID); !errors.Is(err, ErrUpToDate) {
		t.Fatalf("already on it: %v", err)
	}
	if err := s.RequestUpdate(ctx, userA+1, dev.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's stand: %v", err)
	}
	if err := s.CancelUpdate(ctx, userA+1, dev.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancel someone else's: %v", err)
	}
	s.Firmware = ""
	if err := s.RequestUpdate(ctx, userA, dev.ID); !errors.Is(err, ErrNoFirmware) {
		t.Fatalf("nothing bundled: %v", err)
	}
}

func TestAnUpdateBeingInstalledCannotBeCancelled(t *testing.T) {
	s, userA, dev, _ := standOn(t, "0.5.0")
	ctx := context.Background()
	s.RequestUpdate(ctx, userA, dev.ID)
	s.TakeUpdate(ctx, dev.ID)
	s.CancelUpdate(ctx, userA, dev.ID)
	if list, _ := s.List(ctx, userA); list[0].Update == nil || list[0].Update.State != UpdateInstalling {
		t.Fatalf("%+v", list[0].Update)
	}
}
