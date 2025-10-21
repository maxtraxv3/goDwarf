package main

import (
	"strings"
	"testing"

	"gothoom/climg"
	"gothoom/eui"
)

func TestInventoryWindowIncrementalUpdates(t *testing.T) {
	resetInventory()

	oldImages := clImages
	defer func() { clImages = oldImages }()
	clImages = testCLImages(map[uint32]*climg.ClientItem{
		100: {Name: "shadow bell", Slot: kItemSlotRightHand},
		200: {Name: "linen shirt", Slot: kItemSlotTorso},
	})

	inventoryWin = nil
	inventoryList = nil
	inventoryRowRefs = map[*eui.ItemData]invRef{}
	invRender = inventoryRenderState{}
	invBoldSrc = nil
	invItalicSrc = nil
	selectedInvID = 0
	selectedInvIdx = -1

	makeInventoryWindow()

	addInventoryItem(100, -1, "shadow bell", false)
	addInventoryItem(200, -1, "linen shirt", false)
	updateInventoryWindow()

	if inventoryList == nil {
		t.Fatal("inventory list not initialized")
	}
	initial := append([]*eui.ItemData(nil), inventoryList.Contents...)
	if len(initial) < 3 {
		t.Fatalf("expected at least 2 rows and spacer, got %d", len(initial))
	}
	firstRow := initial[0]
	secondRow := initial[1]
	spacer := initial[len(initial)-1]

	addInventoryItem(100, -1, "shadow bell", false)
	updateInventoryWindow()

	if len(inventoryList.Contents) != len(initial) {
		t.Fatalf("expected %d items, got %d", len(initial), len(inventoryList.Contents))
	}
	if inventoryList.Contents[0] != firstRow {
		t.Fatalf("expected first row to be reused")
	}
	if inventoryList.Contents[1] != secondRow {
		t.Fatalf("expected second row to be reused")
	}
	if inventoryList.Contents[len(inventoryList.Contents)-1] != spacer {
		t.Fatalf("expected spacer to be reused")
	}
	if len(firstRow.Contents) < 2 {
		t.Fatalf("expected name text in first row")
	}
	if !strings.Contains(firstRow.Contents[1].Text, "(2)") {
		t.Fatalf("expected quantity suffix in first row text, got %q", firstRow.Contents[1].Text)
	}

	removeInventoryItem(100, -1)
	updateInventoryWindow()

	if inventoryList.Contents[0] != firstRow {
		t.Fatalf("expected first row pointer to remain after decrement")
	}
	if strings.Contains(firstRow.Contents[1].Text, "(2)") {
		t.Fatalf("expected quantity suffix to be removed")
	}

	removeInventoryItem(100, -1)
	updateInventoryWindow()

	if len(inventoryList.Contents) != 2 {
		t.Fatalf("expected one row plus spacer after removal, got %d", len(inventoryList.Contents))
	}
	if inventoryList.Contents[0] != secondRow {
		t.Fatalf("expected remaining row pointer to persist")
	}

	equipInventoryItem(200, -1, true)
	updateInventoryWindow()

	if len(inventoryList.Contents) != 2 {
		t.Fatalf("expected row count to remain stable after equip")
	}
	if inventoryList.Contents[0] != secondRow {
		t.Fatalf("expected equip to reuse existing row pointer")
	}
	if len(secondRow.Contents) < 3 {
		t.Fatalf("expected slot label to be present when equipped")
	}
	if got := secondRow.Contents[len(secondRow.Contents)-1].Text; got != "[Torso]" {
		t.Fatalf("expected slot label [Torso], got %q", got)
	}

	equipInventoryItem(200, -1, false)
	updateInventoryWindow()

	if len(secondRow.Contents) != 2 {
		t.Fatalf("expected slot label removed after unequip")
	}
	if len(inventoryRowRefs) != 1 {
		t.Fatalf("expected row refs to contain single entry, got %d", len(inventoryRowRefs))
	}
}
