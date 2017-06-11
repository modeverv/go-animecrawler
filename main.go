package main

/*
実装方針
とりまDB登録とかしちゃうのはやめておいて、
logger作るのはやめておいて
JSONから設定読み込んで出力するとか
goqueryで一回叩いてみるとか、その辺を2017/06/10はするところまで

チャンネルとかデータ構造とかは2017/06/11から少しずつやっていけばよろし。
DB以外の部分はとりあえず出来た。
めっちゃ速いwwww
rubyだと900秒
goだと2.3秒
*/

import (
	"encoding/json"
	f "fmt"
	"github.com/PuerkitoBio/goquery"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const StartURL = "http://youtubeanisoku1.blog106.fc2.com/"
const Configfile = "config.json"

const (
	ANISOKUTOP int = iota
	KOUSINPAGE
	KOBETUPAGE
	HIMADOSEARCH
	HIMADOVIDEO
)

type Config struct {
	DonloadDir  string `json:"downloaddir"`
	DBFILE      string `json:"dbfile"`
	TITLEREGEXP string `json:"title_regexp"`
}

var cfg Config

type JOB struct {
	JOBKIND int
	URL     string
	TITLE   string
	EPISODE string
}

func (job *JOB) Dispacher() {
	switch job.JOBKIND {
	case ANISOKUTOP:
		wg.Add(1)
		go job.AnisokuTop()
	case KOUSINPAGE:
		wg.Add(1)
		go job.KousinPage()
	case KOBETUPAGE:
		wg.Add(1)
		go job.KobetuPage()
	case HIMADOSEARCH:
		wg.Add(1)
		go job.HimadoSearch()
	case HIMADOVIDEO:
		wg.Add(1)
		go job.HimadoVideo()
	default:
	}

}

// トップページのスクレイピング
func (job *JOB) AnisokuTop() {
	defer wg.Done()
	doc, err := goquery.NewDocument(job.URL)
	if err != nil {
		f.Println("url scraping fail:", job.URL)
		return
	}
	doc.Find(".Top_info div ul li a").Each(func(_ int, s *goquery.Selection) {
		title, _ := s.Attr("title")
		if strings.Contains(title, "更新状況") {
			u, _ := s.Attr("href")
			JobCh <- &JOB{KOUSINPAGE, u, "", ""}
		}
	})
}

// 更新ページのスクレイピング
func (job *JOB) KousinPage() {
	defer wg.Done()
	doc, err := goquery.NewDocument(job.URL)
	if err != nil {
		f.Println("kousinpage error", job.URL)
		return
	}
	doc.Find("ul[type='square'] li a").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if href == "" {
			return
		}
		title, _ := s.Attr("title")
		if title != "" {
			return
		}
		JobCh <- &JOB{KOBETUPAGE, href, "", ""}
	})
}

// 個別ページのスクレイピング
func (job *JOB) KobetuPage() {
	defer wg.Done()
	doc, err := goquery.NewDocument(job.URL)
	if err != nil {
		f.Println("kobetupage error", job.URL)
		return
	}
	var title string
	doc.Find("title").Each(func(_ int, s *goquery.Selection) {
		title = s.Text()
		title = cleanupValue(title)
	})

	if !TitleRegexp.MatchString(title) {
		return
	}

	_, ok := TitleMap[title]
	if ok {
		return
	} else {
		TitleMap[title] = true
	}

	//f.Println("DO:", title)

	found := false
	doc.Find("a").Each(func(_ int, s *goquery.Selection) {
		if found {
			return
		}
		href, _ := s.Attr("href")
		if !strings.Contains(href, "himado.in") {
			return
		}
		//一つ見つかればOK
		if !found {
			JobCh <- &JOB{HIMADOSEARCH, href, title, ""}
			found = true
		}
	})
}

// ひまわり検索ページ
func (job *JOB) HimadoSearch() {
	defer wg.Done()
	doc, err := goquery.NewDocument(job.URL)
	if err != nil {
		f.Println("himadosearch error", job.URL)
		return
	}

	count := 0
	doc.Find(".thumbtitle a[rel='nofollow']").Each(func(_ int, s *goquery.Selection) {
		if count > 2 {
			return
		}
		href, _ := s.Attr("href")
		if href == "" {
			return
		}
		href = "http://himado.in" + href
		// 再取得制御
		_, exist := PageMap[href]
		if exist {
			return
		}
		PageMap[href] = true
		episode, _ := s.Attr("title")
		if episode == "" {
			return
		}
		episode = cleanupValue(episode)
		JobCh <- &JOB{HIMADOVIDEO, href, job.TITLE, episode}
		count++
	})
}

// ひまわりビデオページ
func (job *JOB) HimadoVideo() {
	defer wg.Done()
	doc, err := goquery.NewDocument(job.URL)
	if err != nil {
		f.Println("himadoVideo error", job.URL)
		return
	}
	mediaUrl := ""
	doc.Find("script").Each(func(_ int, s *goquery.Selection) {
		text := s.Contents().Text()
		if text == "" {
			return
		}
		texta := strings.Split(text, "\n")
		for _, l := range texta {
			if strings.Contains(l, "var movie_url") {
				l = strings.TrimSpace(l)
				l = strings.Replace(l, "var movie_url = '", "", -1)
				l = strings.Replace(l, "';", "", -1)
				u, err := url.PathUnescape(l)
				if err == nil {
					mediaUrl = u
				}
				break
			}
		}
	})
	if mediaUrl == "" {
		return
	}
	fp := makeFilePath(job.TITLE, job.EPISODE)
	if !FileIsExists(fp) {
		return
	}
	job.DownloadVideo(mediaUrl)
}

// ビデオダウンロード
func (job *JOB) DownloadVideo(url string) {
	err := os.MkdirAll(makeFileDirPath(job.TITLE), 0777)
	if err != nil {
		f.Println("ディレクトリ作成失敗")
		return
	}
	fp := makeFilePath(job.TITLE, job.EPISODE)
	cmd := "curl -# -L " + url + " | ffmpeg -threads 4 -y -i - -vcodec copy -acodec copy '" + fp + "' &"
	f.Println(cmd)
	exec.Command("sh", "-c", cmd).Start()
}

// ディレクトリを確認
func makeFileDirPath(title string) string {
	return filepath.Join(cfg.DonloadDir, title)
}

// ファイルパスを作成
func makeFilePath(title string, episode string) string {
	return filepath.Join(cfg.DonloadDir, title, episode+".mp4")
}

// ファイル存在確認
func FileIsExists(filename string) bool {
	_, err := os.Stat(filename)
	return err != nil
}

// 値をきれいにする
func cleanupValue(s string) string {
	s = strings.Replace(s, "★ You Tube アニ速 ★", "", -1)
	s = strings.Replace(s, ":", "：", -1)
	s = strings.Replace(s, "第", "", -1)
	s = strings.Replace(s, "話", "：", -1)
	s = strings.Replace(s, ".", "", -1)
	s = strings.Replace(s, "　", "", -1)
	s = strings.Replace(s, " ", "", -1)
	s = strings.Replace(s, "#", "", -1)
	s = strings.Replace(s, "(", "", -1)
	s = strings.Replace(s, ")", "", -1)
	s = strings.Replace(s, "/", "", -1)
	s = strings.Replace(s, "（", "", -1)
	s = strings.Replace(s, "）", "", -1)
	s = strings.Replace(s, "+", "＋", -1)
	s = strings.Replace(s, "[720p]", "", -1)
	s = strings.Replace(s, "高画質", "", -1)
	s = strings.Replace(s, "QQ", "", -1)
	s = strings.Replace(s, "?", "？", -1)
	s = strings.Replace(s, "[", "", -1)
	s = strings.Replace(s, "]", "", -1)

	return s
}

// 確認済み管理マップ
var TitleMap map[string]bool = make(map[string]bool)
var PageMap map[string]bool = make(map[string]bool)
var TitleRegexp *regexp.Regexp

// JOBチャネル
var JobCh chan *JOB = make(chan *JOB)

// WaitGroup
var wg sync.WaitGroup = sync.WaitGroup{}

// コンフィグを読み出す
func loadConfig() (*Config, error) {
	fh, err := os.Open(Configfile)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	err = json.NewDecoder(fh).Decode(&cfg)
	TitleRegexp = regexp.MustCompile(cfg.TITLEREGEXP)
	return &cfg, err
}

// JOBチャネルのレシーバー
func receiver(ch chan *JOB) {
	for {
		job := <-ch
		job.Dispacher()
	}
}

// 本体
func Run() int {

	// コンフィグ読み出し
	cfg, err := loadConfig()
	if err != nil {
		f.Println("config load error", err)
		return 1
	}
	f.Println(cfg)

	go receiver(JobCh)

	// 初期キック
	JobCh <- &JOB{ANISOKUTOP, StartURL, "", ""}

	wg.Wait()
	return 0
}

func main() {
	retcode := Run()
	os.Exit(retcode)
}
