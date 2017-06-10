package main

/*
実装方針
とりまDB登録とかしちゃうのはやめておいて、
logger作るのはやめておいて
JSONから設定読み込んで出力するとか
goqueryで一回叩いてみるとか、その辺を2017/06/10はするところまで

チャンネルとかデータ構造とかは2017/06/11から少しずつやっていけばよろし。

*/

import (
	"encoding/json"
	f "fmt"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const StartURL = "http://youtubeanisoku1.blog106.fc2.com/"
const Configfile = "config.json"

const (
	KOUSINPAGE int = iota
	KOBETUPAGE
	HIMADOSEARCH
	HIMADOVIDEO
)

type Config struct {
	DonloadDir  string `json:"downloaddir"`
	DBFILE      string `json:"dbfile"`
	TITLEREGEXP string `json:"title_regexp"`
}

type JOB struct {
	JOBKIND int
	URL     string
}

var JobCh chan *JOB

// コンフィグを読み出す
func loadConfig() (*Config, error) {
	f, err := os.Open(Configfile)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var cfg Config
	err = json.NewDecoder(f).Decode(&cfg)
	return &cfg, err
}

func dispatcher(ch <-chan *JOB) {
	for {
		job := <-ch
		f.Println(job)
	}
}

// 本体
func Run() int {
	// コンフィグ読み出し
	cfg, err := loadConfig()
	if err != nil {
		f.Println("config load error", err)
		os.Exit(1)
	}
	f.Println(cfg)

	// JOBのチャンネル
	JobCh = make(chan *JOB)
	go dispatcher(JobCh)

	// 初期キック
	doc, err := goquery.NewDocument(StartURL)
	if err != nil {
		f.Println("url scraping fail:", StartURL)
	}
	doc.Find(".Top_info div ul li a").Each(func(_ int, s *goquery.Selection) {
		title, _ := s.Attr("title")
		if strings.Contains(title, "更新状況") {
			url, _ := s.Attr("href")
			JobCh <- &JOB{KOUSINPAGE, url}
		}
	})

	return 0
}

func main() {
	retcode := Run()
	os.Exit(retcode)
}
