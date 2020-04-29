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
	if b64Text == "" {
		w.Write([]byte("Invalid query parameter"))
		return
	}
	b64, err := base64.StdEncoding.DecodeString(b64Text)
	if err != nil {
		w.Write([]byte("Base64 decode error"))
		return
	}
	text := string(b64)
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

		err := b.Answer(q, &tb.QueryResponse{
			Results: tb.Results{
				&tb.AudioResult{
					ResultBase: tb.ResultBase{
						ID: uniTextMd5,
					},
					Title: q.Text,
					URL: fmt.Sprintf(
						"https://pepega.nyodev.xyz/audio?text=%s",
						base64.StdEncoding.EncodeToString([]byte(uniText)),
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
	log.Println("Bot listening")
	b.Start()
}
