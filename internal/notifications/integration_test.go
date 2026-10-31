package notifications_test

import (
	"context"
	"testing"

	"voxhold-backend/internal/channel"
	channelsqlite "voxhold-backend/internal/channel/sqlite"
	"voxhold-backend/internal/message"
	messagesqlite "voxhold-backend/internal/message/sqlite"
	"voxhold-backend/internal/notifications"
	"voxhold-backend/internal/realtime"
	"voxhold-backend/internal/testdb"
)

func TestUnsubscribedChannelNotificationReachesBothTabsAndHonorsRevocation(t *testing.T) {
	db := testdb.Open(t)
	ctx := context.Background()
	repo := channelsqlite.NewRepository(db)
	if _, err := db.Exec(`INSERT INTO channels(id,server_id,name,kind,created_by) VALUES(3,1,'Other','text',1)`); err != nil {
		t.Fatal(err)
	}
	hub := realtime.NewHub()
	hub.SetChannelAccessLoader(repo.AccessibleChannelIDs)
	defer hub.Close()
	var deliveryError error
	service := notifications.NewService(db, hub.IsUserOnline, func(id int64, event notifications.Event) {
		hub.PublishToUser(id, realtime.OutgoingEvent{Type: realtime.EventNotificationMessage, Data: event})
	}, nil)
	publisher := realtime.NewMessageEventPublisher(hub)
	publisher.SetCreatedListener(func(value message.Message) { deliveryError = service.Deliver(value) })
	messages := message.NewService(messagesqlite.NewRepository(db), publisher)
	channels := channel.NewService(repo, realtime.NewChannelEventPublisher(hub))
	first := realtime.NewClient(2, "first-tab", []int64{1})
	second := realtime.NewClient(2, "second-tab", []int64{1})
	outsider := realtime.NewClient(3, "outsider", []int64{1})
	for _, client := range []*realtime.Client{first, second, outsider} {
		if !hub.Register(client) {
			t.Fatal("register")
		}
	}
	hub.Subscribe(first, 1, 3)
	hub.Subscribe(second, 1, 3)
	if _, err := service.Set(ctx, 1, 2, 2, notifications.All); err != nil {
		t.Fatal(err)
	}
	count := func(client *realtime.Client) int {
		count := 0
		for {
			event, ok := client.PopOutgoing()
			if !ok {
				break
			}
			if event.Type == realtime.EventNotificationMessage {
				count++
			}
		}
		return count
	}
	send := func(key string) {
		t.Helper()
		if _, err := messages.Create(ctx, 1, 2, 1, message.CreateInput{Content: "hello <@2>", ClientMessageID: key}); err != nil {
			t.Fatal(err)
		}
		if deliveryError != nil {
			t.Fatal(deliveryError)
		}
	}
	send("first")
	if count(first) != 1 || count(second) != 1 || count(outsider) != 0 {
		t.Fatal("delivery missed an inactive tab or leaked to an outsider")
	}
	send("first")
	if count(first) != 0 || count(second) != 0 {
		t.Fatal("retry notified twice")
	}
	if _, err := channels.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true}); err != nil {
		t.Fatal(err)
	}
	send("second")
	if count(first) != 0 || count(second) != 0 {
		t.Fatal("private channel leaked")
	}
	if _, err := channels.SetAccess(ctx, 1, 2, 1, channel.Access{Restricted: true, UserIDs: []int64{2}}); err != nil {
		t.Fatal(err)
	}
	send("third")
	if count(first) != 1 || count(second) != 1 {
		t.Fatal("grant did not restore delivery")
	}
}
