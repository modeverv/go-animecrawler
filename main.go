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
mapへの読み書きがどうじに成る問題はやはり捨て置くことは出来ないのでどうにかすることにした
*/

import (
	"bytes"
	"database/sql"
	"encoding/json"
	f "fmt"
	"io/ioutil"
	"net/http"
	"net/http/cookiejar"
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

// スクレイピングスタートURL
const StartURL = "http://youtubeanisoku1.blog106.fc2.com/"
// コンフィグファイル
const Configfile = "config.json"
// SQL(インサート)
const InsertSQL = `insert into crawler(name, path, url) values(?, ?, ?)`

// ジョブ種類
const (
	JOBANISOKUTOP   int = iota
	JOBKOUSINPAGE
	JOBKOBETUPAGE
	JOBHIMADOSEARCH
	JOBHIMADOVIDEO
)

// 設定 struct
type Config struct {
	DonloadDir  string `json:"downloaddir"`
	DBFILE      string `json:"dbfile"`
	TITLEREGEXP string `json:"title_regexp"`
}

// 設定
var cfg Config

// JOB struct
type JOB struct {
	JOBKIND int
	URL     string
	TITLE   string
	EPISODE string
}

func (job *JOB) Dispacher() {
	switch job.JOBKIND {
	case JOBANISOKUTOP:
		wg.Add(1)
		go job.AnisokuTop()
	case JOBKOUSINPAGE:
		wg.Add(1)
		go job.KousinPage()
	case JOBKOBETUPAGE:
		wg.Add(1)
		go job.KobetuPage()
	case JOBHIMADOSEARCH:
		wg.Add(1)
		go job.HimadoSearch()
	case JOBHIMADOVIDEO:
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
			JobCh <- &JOB{JOBKOUSINPAGE, u, "", ""}
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
		// タイトルがあるaは対象外でOK
		if title != "" {
			return
		}
		JobCh <- &JOB{JOBKOBETUPAGE, href, "", ""}
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

	// タイトル取得
	var title string
	doc.Find("title").Each(func(_ int, s *goquery.Selection) {
		title = s.Text()
		title = cleanupValue(title)
	})

	// タイトルが取得対象でなあい場合はreturn
	if !TitleRegexp.MatchString(title) {
		return
	}

	// 取得対象タイトルマップにあればreturnなければセット
	if getTMap(title) {
		return
	}else{
		setTMap(title)
	}

	// このタイトルについて取得します
	f.Println("DO:", title)

	// ひまわりのURL探してジョブにする
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
			JobCh <- &JOB{JOBHIMADOSEARCH, href, title, ""}
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
		if getPMap(href) {
			return
		}else{
			setPMap(href)
		}

		episode, _ := s.Attr("title")
		if episode == "" {
			return
		}
		episode = cleanupValue(episode)
		JobCh <- &JOB{JOBHIMADOVIDEO, href, job.TITLE, episode}
		count++
	})
}

// ひまわりビデオページ
func (job *JOB) HimadoVideo() {
	defer wg.Done()

	//ここからはcookieがいる模様なので泥臭くいく
	jar, err := cookiejar.New(nil)
	if err != nil {
		f.Println("jar 作成失敗")
	}
	client := &http.Client{Jar: jar}
	res, err := client.Get(job.URL)
	if err != nil {
		f.Println("接続失敗")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		f.Println(res.StatusCode)
		return
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		f.Println("himado video read failed")
		return
	}
	mediaUrl := ""
	key := ""
	lines := strings.Split(string(body), "\n")
	for _, l := range lines {
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
	found := false
	for _, l := range lines {
		if strings.Contains(l, "function getKey()") {
			found = true
		}
		if found && strings.Contains(l, "return") {
			key = strings.TrimSpace(l)
			//	return 'CyVtCVxkF2';
			key = strings.Replace(key, "return '", "", -1)
			key = strings.Replace(key, "';", "", -1)
			break
		}
	}
	if mediaUrl == "" {
		return
	}
	if strings.HasPrefix(mediaUrl, "external:") {
		mediaUrl = convertMedirUrl(mediaUrl, key, client)
	}
	fp := makeFilePath(job.TITLE, job.EPISODE)
	if !FileIsExists(fp) {
		f.Println("already fetched - ", job.TITLE, " - ", job.EPISODE)
		return
	}
	job.InsertToDB(fp)
	job.DownloadVideo(mediaUrl)
}

// fc2対応
func convertMedirUrl(u string, key string, client *http.Client) string {
	u1 := strings.Replace(u, "external:", "", -1)
	splitted := strings.Split(u1, "/")
	id := splitted[len(splitted)-1]
	endpoint := "http://himado.in/fc2/api/fc2Html5MoviePath.php?up_id=" + id + "&gk=" + key + "&test_mode=0"
	res, err := client.Get(endpoint)
	if err != nil {
		f.Println("接続失敗")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		f.Println(res.StatusCode)
		return u
	}
	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		f.Println("convert url failed")
		return u
	}
	buf := bytes.NewBuffer(body)
	xmlstr := buf.String()
	xmls := strings.Split(xmlstr, "</url>")
	ux := strings.Replace(xmls[0], "<url>", "", -1)
	f.Println(ux)
	return ux
}

// DBインサート
func (job *JOB) InsertToDB(filepath string) {
	db, err := sql.Open("sqlite3", cfg.DBFILE)
	defer db.Close()
	if err != nil {
		panic(err)
	}
	_, err = db.Exec(InsertSQL, job.EPISODE, filepath, job.URL)
	if err != nil {
		f.Println("can not open db file", job)
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
	//exec.Command("ls", "-la")
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

// 回避用管理マップ
var Tlock = sync.RWMutex{}
// タイトル別多重取得回避用
var TitleMap map[string]bool = make(map[string]bool)
func getTMap(title string) bool {
	Tlock.Lock()
	defer Tlock.Unlock()
	_,ok := TitleMap[title]
	return ok
}
func setTMap(title string){
	Tlock.Lock()
	defer Tlock.Unlock()
	TitleMap[title] = true
}

// 多重取得回避用マップ
var Plock = sync.RWMutex{}
var PageMap map[string]bool = make(map[string]bool)
func getPMap(url string) bool {
	Plock.Lock()
	defer Plock.Unlock()
	_,ok :=PageMap[url]
	return ok
}
func setPMap(url string) {
	Plock.Lock()
	defer Plock.Unlock()
	PageMap[url] = true
}

// 取得タイトル制限用正規表現
var TitleRegexp *regexp.Regexp

// JOBチャネル
var JobCh chan *JOB = make(chan *JOB)

// WaitGroup
var wg sync.WaitGroup = sync.WaitGroup{}

// DB
var db *sql.DB

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
	JobCh <- &JOB{JOBANISOKUTOP, StartURL, "", ""}

	// 待受
	wg.Wait()
	return 0
}

// エントリーポイント
func main() {
	retcode := Run()
	os.Exit(retcode)
}
