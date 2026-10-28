package db_test

import (
	"testing"
	"time"

	"github.com/ziyan/teanode/internal/db"
	"github.com/ziyan/teanode/internal/db/dbtest"
)

// Which instance holds a computer is found only while it has been heard of
// lately, by name or as any computer of the agent, and only an instance's own
// row is removed by that instance.
func TestAComputerIsFoundOnlyWhileItsInstanceIsHeardOf(test *testing.T) {
	database, closeDatabase := dbtest.AcquireDatabase(test)
	defer closeDatabase()

	now := time.Now()
	freshAfter := now.Add(-90 * time.Second)
	run := func(work func(tx db.Transaction) error) {
		test.Helper()
		if err := database.Transaction(work); err != nil {
			test.Fatal(err)
		}
	}
	find := func(computerName string) string {
		test.Helper()
		var instance string
		run(func(tx db.Transaction) (err error) {
			instance, err = tx.FindAgentComputerAttachment("agent-a", computerName, freshAfter)
			return err
		})
		return instance
	}

	if instance := find("laptop"); instance != "" {
		test.Fatalf("a computer nothing holds is held by %q", instance)
	}
	run(func(tx db.Transaction) error {
		if err := tx.PutAgentComputerAttachment("agent-a", "laptop", "instance-one", now.Add(-time.Hour)); err != nil {
			return err
		}
		if err := tx.PutAgentComputerAttachment("agent-a", "desktop", "instance-two", now.Add(-10*time.Second)); err != nil {
			return err
		}
		return tx.PutAgentComputerAttachment("agent-b", "laptop", "instance-three", now)
	})
	if instance := find("laptop"); instance != "" {
		test.Errorf("a computer whose instance was last heard of an hour ago is held by %q", instance)
	}
	if instance := find("Desktop"); instance != "instance-two" {
		test.Errorf("the desktop, asked for in other letters, is held by %q", instance)
	}
	if instance := find(""); instance != "instance-two" {
		test.Errorf("any computer of the agent is held by %q, not the one heard of lately", instance)
	}

	// The laptop attaches again to another instance, which takes the row, and
	// the first instance's late detach leaves the new row alone.
	run(func(tx db.Transaction) error {
		if err := tx.PutAgentComputerAttachment("agent-a", "laptop", "instance-two", now); err != nil {
			return err
		}
		return tx.DeleteAgentComputerAttachment("agent-a", "laptop", "instance-one")
	})
	if instance := find("laptop"); instance != "instance-two" {
		test.Errorf("the laptop is held by %q after it attached to the second instance", instance)
	}
	if instance := find(""); instance == "" {
		test.Error("no computer of the agent is found")
	}

	// The newest of several is the one any computer means.
	run(func(tx db.Transaction) error {
		return tx.PutAgentComputerAttachment("agent-a", "tablet", "instance-one", now.Add(time.Second))
	})
	if instance := find(""); instance != "instance-one" {
		test.Errorf("any computer is held by %q, not by the instance heard of most recently", instance)
	}

	// Heartbeats keep an instance's rows fresh, and starting forgets them.
	later := now.Add(time.Hour)
	run(func(tx db.Transaction) error { return tx.HeartbeatAgentComputerAttachments("instance-two", later) })
	freshAfter = later.Add(-time.Minute)
	if instance := find("desktop"); instance != "instance-two" {
		test.Errorf("a heartbeat did not keep the desktop held: %q", instance)
	}
	if instance := find("tablet"); instance != "" {
		test.Errorf("a heartbeat of another instance kept the tablet held by %q", instance)
	}
	run(func(tx db.Transaction) error { return tx.DeleteAgentComputerAttachmentsOfInstance("instance-two") })
	if instance := find("desktop"); instance != "" {
		test.Errorf("the desktop is still held by %q after its instance forgot its computers", instance)
	}
}
