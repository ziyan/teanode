package telegram

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/ziyan/teanode/internal/channel"
)

// Bot is a Telegram bot as the channel manager runs it: updates polled
// and handed over, and a Chat per conversation to answer through.
type Bot struct {
	client   *Client
	identity *User
}

// Open is a channel.Opener for Telegram: the token is tried at once, so
// a wrong one is an error now rather than a bot that never answers.
func Open(ctx context.Context, token string) (channel.Bot, error) {
	client := New(token, nil, "")
	identity, err := client.Me(ctx)
	if err != nil {
		return nil, err
	}
	commands, order := channel.Commands()
	_ = client.SetCommands(ctx, commands, order)
	return &Bot{client: client, identity: identity}, nil
}

// OpenAt is Open against another address, for tests.
func OpenAt(ctx context.Context, token, base string) (channel.Bot, error) {
	client := New(token, nil, base)
	identity, err := client.Me(ctx)
	if err != nil {
		return nil, err
	}
	return &Bot{client: client, identity: identity}, nil
}

func (self *Bot) Name() string { return self.identity.Name() }

// Run polls for updates until the context ends. Telegram refuses a
// second poller on the same token; that refusal ends the run, and the
// manager retries on a later tick when the claim allows.
func (self *Bot) Run(ctx context.Context, handle func(ctx context.Context, incoming *channel.Incoming, chat channel.Chat)) error {
	offset := int64(0)
	failures := 0
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		updates, err := self.client.Poll(ctx, offset, 50*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var refused *Error
			if errors.As(err, &refused) && (refused.Code == 401 || refused.Code == 409) {
				return err
			}
			failures++
			if failures > 20 {
				return err
			}
			select {
			case <-time.After(time.Duration(failures) * 2 * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		failures = 0
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			if update.Message == nil || (update.Message.From != nil && update.Message.From.IsBot) {
				continue
			}
			message := update.Message
			incoming := self.incoming(message)
			if incoming.Text == "" && len(incoming.Files) == 0 {
				continue
			}
			go handle(ctx, incoming, &chat{client: self.client, chatId: message.Chat.ID})
		}
	}
}

func (self *Bot) incoming(message *Message) *channel.Incoming {
	incoming := &channel.Incoming{
		ChatID:    strconv.FormatInt(message.Chat.ID, 10),
		ChatName:  message.ChatName(),
		MessageID: strconv.FormatInt(message.MessageID, 10),
		Text:      message.Said(),
		Group:     message.Group(),
	}
	if message.From != nil {
		incoming.SenderID = strconv.FormatInt(message.From.ID, 10)
		incoming.SenderName = message.From.Name()
	}
	if message.ReplyToMessage != nil && message.ReplyToMessage.From != nil && message.ReplyToMessage.From.ID == self.identity.ID {
		incoming.ToBot = true
	}
	if self.identity.Username != "" && strings.Contains(incoming.Text, "@"+self.identity.Username) {
		incoming.ToBot = true
	}
	for _, file := range message.Attachments() {
		fileId := file.FileID
		incoming.Files = append(incoming.Files, channel.IncomingFile{
			Name: file.FileName, ContentType: file.MimeType,
			Fetch: func(ctx context.Context) ([]byte, error) { return self.client.Download(ctx, fileId) },
		})
	}
	return incoming
}

// chat is one Telegram chat as the turn writes into it.
type chat struct {
	client *Client
	chatId int64
}

func (self *chat) Send(ctx context.Context, text, replyTo string) (string, error) {
	reply, _ := strconv.ParseInt(replyTo, 10, 64)
	id, err := self.client.Send(ctx, self.chatId, text, reply, true)
	if err != nil {
		return "", err
	}
	return strconv.FormatInt(id, 10), nil
}

func (self *chat) Edit(ctx context.Context, messageId, text string) error {
	id, err := strconv.ParseInt(messageId, 10, 64)
	if err != nil {
		return err
	}
	return self.client.Edit(ctx, self.chatId, id, text, true)
}

func (self *chat) Delete(ctx context.Context, messageId string) error {
	id, err := strconv.ParseInt(messageId, 10, 64)
	if err != nil {
		return err
	}
	return self.client.Delete(ctx, self.chatId, id)
}

func (self *chat) Typing(ctx context.Context) error {
	return self.client.Typing(ctx, self.chatId)
}

func (self *chat) SendFile(ctx context.Context, name, contentType string, content []byte, caption string) error {
	return self.client.SendFile(ctx, self.chatId, name, contentType, content, caption)
}

func (self *chat) Limit() int { return MessageLimit }
