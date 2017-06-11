package main

/*
実装方針
とりまDB登録とかしちゃうのはやめておいて、
logger作るのはやめておいて→標準出力をうまいことする
JSONから設定読み込んで出力するとか
goqueryで一回叩いてみるとか、その辺を2017/06/10はするところまで

チャンネルとかデータ構造とかは2017/06/11から少しずつやっていけばよろし。
めっちゃ速いwwww
rubyだと900秒
goだと4.5秒
DB入れた。10分でできたねー
5.3秒(安全のためにDBのオープンクローズを毎回しているのでそのあたりが
重いのだろう。
それでもこの値はめちゃくちゃすばらしい。
*/

import (
	"database/sql"
	"encoding/json"
	f "fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/PuerkitoBio/goquery"
	_ "github.com/mattn/go-sqlite3"
)

const StartURL = "http://youtubeanisoku1.blog106.fc2.com/"
const Configfile = "config.json"
const InsertSQL = `insert into crawler(name, path, url) values(?, ?, ?)`

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
		f.Println("already fetched - ", job.TITLE, " - ", job.EPISODE)
		return
	}
	job.InsertToDB(fp)
	job.DownloadVideo(mediaUrl)
}

// DBインサート
func (job *JOB) InsertToDB(filepath string) {
	db, err := sql.Open("sqlite3", cfg.DBFILE)
	defer db.Close()
	if err != nil {
		f.Println("can not open db file")
		return
	}
	_, err = db.Exec(InsertSQL, job.EPISODE, filepath, job.URL)
	if err != nil {
		f.Println("can not open db file" , job)
	}
}

// ビデオダウンロード
func (job *JOB) DownloadVideo(url string) {
	err := os.MkdirAll(makeFileDirPath(job.TITLE), 0777)
	if err != nil {
		f.Println("make directory failed")
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

// 多重取得回避用管理マップ(コレ、goroutine間でもOKなんでしょうか？ぱっと動かしてる感じちゃんと動いているけど
// タイトル別多重取得回避用
var TitleMap map[string]bool = make(map[string]bool)

// ページ別多重取得回避用マップ
var PageMap map[string]bool = make(map[string]bool)

// 取得タイトル制限用正規表現
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

	return &cfg, err
}

// DBの用意をする
func setupDB() {
	db, err := sql.Open("sqlite3", cfg.DBFILE)
	defer db.Close()
	if err != nil {
		panic(err)
	}
	createTablesql := `
      CREATE TABLE IF NOT EXISTS crawler(
        id integer primary key,
        name text,
        path text,
        created_at TIMESTAMP DEFAULT (DATETIME('now','localtime')),
        url text
      );
	`
	_, err = db.Exec(createTablesql)
	if err != nil {
		panic(err)
	}
	rows, err := db.Query(`select url from crawler`)
	u := ""
	if err != nil {
		for rows.Next() {
			err = rows.Scan(&u)
			if err != nil {
				panic(err)
			}
			PageMap[u] = true
		}
	}
}

// JOBチャネルのレシーバー
func receiver(ch chan *JOB) {
	for {
		job := <-ch
		job.Dispacher()
	}
}

// 処理本体
func Run() int {

	// コンフィグ読み出し
	cfg, err := loadConfig()
	if err != nil {
		f.Println("config load error", err)
		return 1
	}
	f.Println(cfg)

	// 取得タイトル制限用正規表現のコンパイル
	TitleRegexp = regexp.MustCompile(cfg.TITLEREGEXP)

	// DBの用意をする
	setupDB()

	// レシーバー
	go receiver(JobCh)

	// 初期キック
	JobCh <- &JOB{ANISOKUTOP, StartURL, "", ""}

	// 待受
	wg.Wait()
	return 0
}

// エントリーポイント
func main() {
	retcode := Run()
	os.Exit(retcode)
}
