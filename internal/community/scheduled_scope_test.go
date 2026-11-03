package community

import (
	"encoding/json"
	"net/url"
	"testing"
)

func TestScheduledMessagesFilterDestinationBeforePagination(t *testing.T) {
	f := setup(t)
	if _, err := f.db.Exec(`INSERT INTO scheduled_messages(id,user_id,destination_kind,destination_id,payload,send_at,status) VALUES
 (101,1,'conversation',2,'{"content":"direct"}',30,'pending'),
 (102,1,'channel',2,'{"content":"latest"}',20,'pending'),
 (103,1,'channel',2,'{"content":"earlier"}',10,'failed'),
 (104,2,'channel',2,'{"content":"other user"}',40,'pending'),
 (105,1,'channel',3,'{"content":"other channel"}',50,'pending')`); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/me/scheduled-messages?destination_kind=channel&destination_id=2&limit=1"
	first := f.call(t, 1, "GET", base, nil, 200)
	var values []Scheduled
	if err := json.Unmarshal(first["scheduled_messages"], &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].ID != 102 || string(first["has_more"]) != "true" {
		t.Fatalf("first page: %s", first)
	}
	var cursor string
	if err := json.Unmarshal(first["next_cursor"], &cursor); err != nil {
		t.Fatal(err)
	}
	second := f.call(t, 1, "GET", base+"&before="+url.QueryEscape(cursor), nil, 200)
	if err := json.Unmarshal(second["scheduled_messages"], &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].ID != 103 || string(second["has_more"]) != "false" {
		t.Fatalf("second page: %s", second)
	}
	direct := f.call(t, 1, "GET", "/api/v1/me/scheduled-messages?destination_kind=conversation&destination_id=2&limit=10", nil, 200)
	if err := json.Unmarshal(direct["scheduled_messages"], &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].ID != 101 {
		t.Fatalf("destination kind ignored: %+v", values)
	}
	legacy := f.call(t, 1, "GET", "/api/v1/me/scheduled-messages?limit=10", nil, 200)
	if err := json.Unmarshal(legacy["scheduled_messages"], &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 4 {
		t.Fatalf("legacy list changed or included another user's data: %+v", values)
	}
}

func TestScheduledMessagesRejectIncompleteDestinationFilters(t *testing.T) {
	f := setup(t)
	for _, query := range []string{"destination_kind=channel", "destination_id=2", "destination_kind=server&destination_id=2", "destination_kind=channel&destination_id=-1", "destination_kind=channel&destination_id=abc"} {
		f.call(t, 1, "GET", "/api/v1/me/scheduled-messages?limit=10&"+query, nil, 400)
	}
}
