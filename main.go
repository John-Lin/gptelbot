package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/kelseyhightower/envconfig"
	openai "github.com/sashabaranov/go-openai"
)

type Config struct {
	OpenAIToken   string
	TelegramToken string
	ChatID        int64
}

type TelegramChatSession struct {
	History      []string
	ChatHistory  map[int64][]string
	OpenAIClient *openai.Client
	SessionKey   int64
	IsGroup      bool
}

func main() {
	var c Config
	err := envconfig.Process("gptelbot", &c)
	if err != nil {
		log.Fatal(err.Error())
	}

	if c.OpenAIToken == "" || c.TelegramToken == "" || c.ChatID == 0 {
		log.Printf("OpenAIToken: %s", c.OpenAIToken)
		log.Printf("TelegramToken: %s", c.TelegramToken)
		log.Printf("ChatID: %d", c.ChatID)
		log.Panicln("Please set token and chat id first")
	}

	tgc := &TelegramChatSession{
		OpenAIClient: openai.NewClient(c.OpenAIToken),
		ChatHistory:  make(map[int64][]string, 20),
	}

	bot, err := tgbotapi.NewBotAPI(c.TelegramToken)
	if err != nil {
		log.Panic(err)
	}

	bot.Debug = true

	log.Printf("Authorized on account %s", bot.Self.UserName)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)
	for update := range updates {
		if update.Message == nil { // ignore any non-Message updates
			continue
		}

		if !update.Message.IsCommand() { // ignore any non-command Messages
			continue
		}

		// Create a new MessageConfig. We don't have text yet,
		// test
		// so we leave it empty.
		msg := tgbotapi.NewMessage(c.ChatID, "")

		// Extract the command from the Message.
		switch update.Message.Command() {
		case "help":
			msg.Text = "I understand /gpt, /mode, /flush."
		case "mode":
			mode := update.Message.CommandArguments()
			if mode == "group" {
				tgc.IsGroup = true
				msg.Text = "Group Chat Context Mode"
			} else if mode == "!group" {
				tgc.IsGroup = false
				msg.Text = "Individual Chat Context Mode"
			} else {
				msg.Text = "I don't know that command"
			}
		case "gpt":
			log.Printf("%d, %s\n", update.Message.From.ID, update.Message.From.UserName)
			if tgc.IsGroup {
				tgc.SessionKey = update.Message.Chat.ID
			} else {
				tgc.SessionKey = update.Message.From.ID
			}
			arguments := update.Message.CommandArguments()
			log.Println("ChatID: ", update.Message.Chat.ID)

			msg.Text, err = tgc.prompt(tgc.SessionKey, arguments, true)
			if err != nil {
				log.Printf("%v\n", err)
				msg.Text = "Looks like something went wrong."
			}
			log.Printf("All Chat History: %v", tgc.ChatHistory)
		case "flush":
			msg.Text = "Removing cache..."
			tgc.ChatHistory = make(map[int64][]string, 20)
		case "status":
			msg.Text = "I'm ok."
		default:
			msg.Text = "I don't know that command"
		}

		if _, err := bot.Send(msg); err != nil {
			log.Panic(err)
		}
	}

}

func (c *TelegramChatSession) prompt(from int64, text string, keepHistory bool) (string, error) {
	var chatCompletionMessage []openai.ChatCompletionMessage
	if len(c.ChatHistory[from]) != 0 {
		log.Printf("in context")
		// in context
		prev := strings.Join(c.ChatHistory[from][:], "\n")
		log.Printf("Prev:%s", prev)
		chatCompletionMessage = []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "你是一個聰明的助手，擅長回顧對話，對話中的 user 指的是我， assistant 指的是你",
			},
			{
				Role:    openai.ChatMessageRoleAssistant,
				Content: fmt.Sprintf("Our chat history: %s", prev),
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: text,
			},
		}
	} else {
		log.Printf("first time")
		// begin
		chatCompletionMessage = []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: "你是一個聰明的助手，擅長回顧對話，對話中的 user 指的是我， assistant 指的是你",
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: text,
			},
		}

	}
	resp, err := c.OpenAIClient.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:       openai.GPT3Dot5Turbo,
			Messages:    chatCompletionMessage,
			Temperature: 1,
			TopP:        1,
			MaxTokens:   300,
		},
	)
	if err != nil {
		return "", err
	}
	log.Printf("%v", c.ChatHistory[from])
	if keepHistory {
		c.ChatHistory[from] = append(c.ChatHistory[from], fmt.Sprintf("user: %s assistant: %s\n", text, resp.Choices[0].Message.Content))
	}
	return resp.Choices[0].Message.Content, nil
}
