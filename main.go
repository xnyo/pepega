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
	"github.com/kelseyhightower/envconfig"
	tb "gopkg.in/tucnak/telebot.v2"

	"fmt"
)

// config is the configuration struct
type config struct {
	PublicURL string `required:"true" split_words:"true"`
	BotToken  string `required:"true" split_words:"true"`

	MaxLength      int    `required:"true" default:"64" split_words:"true"`
	AwsEndpoint    string `required:"true" default:"eu-central-1" split_words:"true"`
	AwsFilePath    string `required:"true" default:".aws" split_words:"true"`
	AwsProfile     string `required:"true" default:"pepega" split_words:"true"`
	ListenTo       string `required:"true" default:":7777" split_words:"true"`
	AudioCachePath string `required:"true" default:"audios" split_words:"true"`
}

type cacheEntry struct {
	content  string
	issuedAt time.Time
	ttl      time.Duration
}

// newCachedEntry created a new cachedEntry
func newCachedEntry(s string, ttl time.Duration) *cacheEntry {
	return &cacheEntry{
		issuedAt: time.Now(),
		content:  s,
		ttl:      time.Minute,
	}
}

// expired returns true if the current cacheEntry is expired
func (c *cacheEntry) expired() bool {
	return c.issuedAt.Before(time.Now().Add(c.ttl))
}

// stringMd5 returns the md5 of a string
func stringMd5(s string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(s)))
}

// synthesize uses the provided aws polly client to synthesize a message
func synthesize(
	p *polly.Polly, s string, format string, voiceId string,
) (io.Reader, error) {
	output, err := p.SynthesizeSpeech(&polly.SynthesizeSpeechInput{
		OutputFormat: aws.String(format),
		Text:         aws.String(s),
		VoiceId:      aws.String(voiceId),
	})
	if err != nil {
		return nil, fmt.Errorf("aws polly error: %s", err)
	}
	return output.AudioStream, nil
}

// md5Cache is an md5 -> plain text map,
// because telegram has an URL length limit
// for inline query responses
var md5Cache map[string]*cacheEntry = make(map[string]*cacheEntry)

// pollyClient is the AWS polly client
var pollyClient *polly.Polly

// conf is the environment config
var conf *config

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
			// ????
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

	// Check length
	if len(text) > conf.MaxLength {
		w.Write([]byte("Too long"))
		return
	}

	// Create cache dir if it doesn't exist
	if _, err := os.Stat(conf.AudioCachePath); os.IsNotExist(err) {
		if os.Mkdir(conf.AudioCachePath, os.ModeDir) != nil {
			w.Write([]byte("Could not create cache directory"))
			return
		}
	}

	// Try to get the file
	cacheFilePath := path.Join(conf.AudioCachePath, stringMd5(text)+".mp3")
	if _, err := os.Stat(cacheFilePath); err != nil {
		// LET'S FUCKING GO BOOOOOOOOOOOOOYSSSSSSS
		// The file is not in the cache. Get it from AWS.
		log.Printf("%s (AWS)\n", text)

		// AWS polly
		audioReader, err := synthesize(pollyClient, text, "mp3", "Brian")
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

		// Send the response immediately
		// TeeReader allows us to reuse the stream twice
		var buf bytes.Buffer
		tee := io.TeeReader(audioReader, &buf)
		io.Copy(w, tee)

		// Start a goroutine that saves the file on disk
		go io.Copy(f, &buf)
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
	var conf config
	err := envconfig.Process("pepega", &conf)
	if err != nil {
		log.Fatal(err.Error())
	}

	// Telegram bot
	b, err := tb.NewBot(tb.Settings{
		Token:  conf.BotToken,
		Poller: &tb.LongPoller{Timeout: 10 * time.Second},
	})
	if err != nil {
		log.Fatal(err.Error())
	}

	// Connect to AWS
	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(conf.AwsEndpoint),
		Credentials: credentials.NewSharedCredentials(
			conf.AwsFilePath,
			conf.AwsProfile,
		),
	})
	if err != nil {
		log.Fatal(err.Error())
	}
	pollyClient = polly.New(sess)

	// Register bot handlers
	b.Handle(tb.OnQuery, func(q *tb.Query) {
		uniText := strings.Trim(strings.ToLower(q.Text), " ")
		uniTextMd5 := stringMd5(uniText)
		_, ok := md5Cache[uniTextMd5]
		if !ok {
			md5Cache[uniTextMd5] = newCachedEntry(q.Text, time.Minute)
		}
		if len(uniText) >= conf.MaxLength {
			if b.Answer(q, &tb.QueryResponse{Results: tb.Results{}}) != nil {
				log.Println(err.Error())
			}
			return
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
						"%s/audio?telegram=%s",
						conf.PublicURL,
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

	// HTTP server for telegram inline query URLs
	go func() {
		http.HandleFunc("/audio", serveAudioHandler)
		log.Println("Server started")
		http.ListenAndServe(conf.ListenTo, nil)
	}()

	// Clear cache URLs
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
