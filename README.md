# pepega
### Telegram Inline Bot that's like Streamlab's TTS
This bot uses AWS Polly to play messages via inline queries. That's it.

## Requirements
- An AWS account
- A Telegram bot with inline enabled (/setinline@botfather)
- A web server (optional, but recommended)
- Docker (optional)

## Setting up with docker
```
$ git clone ...
$ cd pepega
$ docker build -t pepega .
$ cat "[pepega]
aws_access_key_id = YOUR_CREDENTIALS
aws_secret_access_key = YOUR_CREDENTIALS" > .aws
$ docker run \
        -d \
        -e "PEPEGA_PUBLIC_URL=https://YOUR.DOMAIN.COM" \
        -e "PEPEGA_BOT_TOKEN=YOUR_TELEGRAM_TOKEN_HERE" \
        -v `pwd`/audios:/app/audios \
        -v `pwd`/.aws:/app/.aws \
        -p 7777:7777 \
        pepega
```

Then configure nginx, like this
```
server {
    server_name your.domain.com;
    location / {
        proxy_pass http://localhost:7777;
    }
}
```
This is needed because telegram needs an URL to an audio file for inline
queries.  
If you don't want to use a web server as a reverse proxy, you can
set the `PEPEGA_PUBLIC_URL` env var to `http://YOUR_IP:7777` and it should
work (as long as you allow incoming TCP connections on port 7777)

## LICENSE
MIT