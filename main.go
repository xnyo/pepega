package main

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/polly"
	tb "gopkg.in/tucnak/telebot.v2"

	"fmt"
)

var pollyClient *polly.Polly

type cachedEntry struct {
	content  string
	issuedAt time.Time
}

func newCachedEntry(s string) *cachedEntry {
	return &cachedEntry{
		issuedAt: time.Now(),
		content:  s,
	}
}

func (c *cachedEntry) expired() bool {
	return c.issuedAt.Before(time.Now().Add(-1 * time.Minute))
}

var md5Cache map[string]*cachedEntry = make(map[string]*cachedEntry)

func stringMd5(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}

func synthesize(p *polly.Polly, s string) (io.Reader, error) {
	output, err := p.SynthesizeSpeech(&polly.SynthesizeSpeechInput{
		OutputFormat: aws.String("mp3"),
		Text:         aws.String(s),
		VoiceId:      aws.String("Brian"),
	})
	if err != nil {
		return nil, fmt.Errorf("SynthesizeSpeech: %s", err)
	}
	return output.AudioStream, nil
}

func serveAudioHandler(w http.ResponseWriter, r *http.Request) {
	b64Text := r.URL.Query().Get("text")

	var text string
	if b64Text != "" {
		// base64
		decoded, err := base64.StdEncoding.DecodeString(b64Text)
		if err != nil {
			w.Write([]byte("Base64 decode error"))
			return
		}
		text = string(decoded)
	} else {
		// telegram md5
		telegramMd5 := r.URL.Query().Get("telegram")
		if telegramMd5 == "" {
			w.Write([]byte("Invalid request"))
			return
		}
		cache, ok := md5Cache[telegramMd5]
		if !ok {
			w.Write([]byte("Unknown md5"))
			return
		}
		text = cache.content
	}

	cacheFilePath := path.Join("audios", stringMd5(text)+".mp3")
	if _, err := os.Stat(cacheFilePath); err != nil {
		log.Printf("%s (AWS)\n", text)
		// Get from AWS
		audioReader, err := synthesize(pollyClient, text)
		if err != nil {
			w.Write([]byte("Synthesize error"))
			return
		}
		f, err := os.Create(cacheFilePath)
		if err != nil {
			w.Write([]byte("Cannot open file (write)"))
			return
		}
		defer f.Close()

		var buf bytes.Buffer
		tee := io.TeeReader(audioReader, &buf)
		io.Copy(f, tee)
		io.Copy(w, &buf)
		return
	}

	// Get from cache
	log.Printf("%s (Cache)\n", text)
	f, err := os.Open(cacheFilePath)
	if err != nil {
		w.Write([]byte("Cannot open file (read)"))
		return
	}
	io.Copy(w, f)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %s telegram_bot_token", os.Args[0])
		os.Exit(1)
	}

	// Telegram bot
	b, err := tb.NewBot(tb.Settings{
		Token:  os.Args[1],
		Poller: &tb.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		panic(err)
	}

	// Connect to AWS
	sess := session.Must(session.NewSession(&aws.Config{
		Region:      aws.String("eu-central-1"),
		Credentials: credentials.NewSharedCredentials(".aws", "pepega"),
	}))
	pollyClient = polly.New(sess)

	// Register bot handlers
	b.Handle(tb.OnQuery, func(q *tb.Query) {
		uniText := strings.Trim(strings.ToLower(q.Text), " ")
		uniTextMd5 := stringMd5(uniText)
		_, ok := md5Cache[uniTextMd5]
		if !ok {
			md5Cache[uniTextMd5] = newCachedEntry(q.Text)
		}
		err := b.Answer(q, &tb.QueryResponse{
			Results: tb.Results{
				&tb.AudioResult{
					ResultBase: tb.ResultBase{
						ID: uniTextMd5,
					},
					Caption: q.Text,
					Title:   "🐸",
					URL: fmt.Sprintf(
						"https://pepega.nyodev.xyz/audio?telegram=%s",
						uniTextMd5,
					),
				},
			},
			CacheTime: 60,
		})
		if err != nil {
			log.Println(err.Error())
		}
	})
	go func() {
		http.HandleFunc("/audio", serveAudioHandler)
		log.Println("Server started")
		http.ListenAndServe(":7777", nil)
	}()
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			log.Println("Clearning md5 cache")
			for k, v := range md5Cache {
				if v.expired() {
					delete(md5Cache, k)
				}
			}
		}
	}()
	log.Println("Bot listening")
	b.Start()
}
