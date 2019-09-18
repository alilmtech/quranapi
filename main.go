package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/boltdb/bolt"
	"github.com/jsteenb2/httpc"
)

func main() {
	db, err := bolt.Open("quran.db", os.ModePerm, bolt.DefaultOptions)
	if err != nil {
		log.Panic(err)
	}
	defer db.Close()

	quranSVC, err := NewQuranService(&http.Client{Timeout: 10 * time.Second}, db)
	if err != nil {
		log.Panic(err)
	}

	deleteChapters := []int{}
	for _, chapter := range deleteChapters {
		if err := quranSVC.deleteChapterDB(chapter); err != nil {
			log.Println(err)
		}
	}

	chapterSummaries, err := quranSVC.ChaptersSummary(context.Background())
	if err != nil {
		log.Panic(err)
	}

	for _, chapterSummary := range chapterSummaries {
		chapter, err := quranSVC.GetChapter(context.Background(), chapterSummary.ID)
		if err != nil {
			log.Println(err)
			continue
		}
		log.Printf("num=%d chapter=%q num_verses=%d", chapter.Number, chapter.NameSimple, len(chapter.Verses))
	}
}

type ChapterSummary struct {
	ID                  int    `json:"id"`
	Number              int    `json:"chapter_number"`
	BismallahPre        bool   `json:"bismillah_pre"`
	RevelationOrder     int    `json:"revelation_order"`
	RevelationPlace     string `json:"revelation_place"`
	NameTransliteration string `json:"name_complex"`
	NameArabic          string `json:"name_arabic"`
	NameSimple          string `json:"name_simple"`
	VerseCount          int    `json:"verses_count"`
	Pages               [2]int `json:"pages"`
	TranslatedName      struct {
		LanguageName string `json:"language_name"`
		Name         string `json:"name"`
	} `json:"translated_name"`
}

func (c ChapterSummary) startPage() int {
	return c.Pages[0]
}

func (c ChapterSummary) endPage() int {
	return c.Pages[1]
}

type Chapter struct {
	ID                  int    `json:"id"`
	Number              int    `json:"chapter_number"`
	BismallahPre        bool   `json:"bismillah_pre"`
	RevelationOrder     int    `json:"revelation_order"`
	RevelationPlace     string `json:"revelation_place"`
	NameTransliteration string `json:"name_complex"`
	NameArabic          string `json:"name_arabic"`
	NameSimple          string `json:"name_simple"`
	Pages               Pages  `json:"pages"`
	TranslatedName      struct {
		LanguageName string `json:"language_name"`
		Name         string `json:"name"`
	} `json:"translated_name"`
	Verses []Verse `json:"verses"`
}

type Pages struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type Verse struct {
	ID           int    `json:"id"`
	VerseNumber  int    `json:"verse_number"`
	ChapterID    int    `json:"chapter_id"`
	VerseKey     string `json:"verse_key"`
	TextMadani   string `json:"text_madani"`
	TextIndopak  string `json:"text_indopak"`
	TextSimple   string `json:"text_simple"`
	JuzNumber    int    `json:"juz_number"`
	HizbNumber   int    `json:"hizb_number"`
	RubNumber    int    `json:"rub_number"`
	Sajdah       string `json:"sajdah"`
	SajdahNumber int    `json:"sajdah_number"`
	PageNumber   int    `json:"page_number"`
	Audio        struct {
		URL      string     `json:"url"`
		Duration int        `json:"duration"`
		Segments [][]string `json:"segments"`
		Format   string     `json:"format"`
	} `json:"audio"`
	Translations []struct {
		ID           int    `json:"id"`
		LanguageName string `json:"language_name"`
		Text         string `json:"text"`
		ResourceName string `json:"resource_name"`
		ResourceID   int    `json:"resource_id"`
	} `json:"translations"`
	MediaContents []struct {
		URL        string `json:"url"`
		EmbedText  string `json:"embed_text"`
		Provider   string `json:"provider"`
		AuthorName string `json:"author_name"`
	} `json:"media_contents"`
	Words []Word `json:"words"`
}

type Word struct {
	ID          int    `json:"id"`
	Position    int    `json:"position"`
	TextMadani  string `json:"text_madani"`
	TextIndopak string `json:"text_indopak"`
	TextSimple  string `json:"text_simple"`
	VerseKey    string `json:"verse_key"`
	ClassName   string `json:"class_name"`
	LineNumber  int    `json:"line_number"`
	PageNumber  int    `json:"page_number"`
	Code        string `json:"code"`
	CodeV3      string `json:"code_v3"`
	CharType    string `json:"char_type"`
	Audio       struct {
		URL string `json:"url"`
	} `json:"audio"`
	Translation struct {
		ID           int    `json:"id"`
		LanguageName string `json:"language_name"`
		Text         string `json:"text"`
		ResourceName string `json:"resource_name"`
		ResourceID   int    `json:"resource_id"`
	} `json:"translation"`
}

type Doer interface {
	Do(*http.Request) (*http.Response, error)
}

type QuranService struct {
	httpClient *httpc.Client
	db         *bolt.DB
}

func NewQuranService(doer Doer, db *bolt.DB) (*QuranService, error) {
	svc := &QuranService{
		httpClient: httpc.New(doer, httpc.WithBaseURL("http://staging.quran.com:3000/api/v3")),
		db:         db,
	}

	if err := svc.initDB(); err != nil {
		return nil, err
	}

	return svc, nil
}

func (q *QuranService) GetChapter(ctx context.Context, id int) (Chapter, error) {
	chapter, err := q.getChapterDB(ctx, id)
	if err == nil {
		return chapter, nil
	}

	chapter, err = q.getChapter(ctx, id)
	if err != nil {
		return Chapter{}, err
	}

	if err := q.setChapterDB(chapter); err != nil {
		log.Println(err)
	}

	return chapter, nil
}

func (q *QuranService) getChapterSummary(ctx context.Context, id int) (ChapterSummary, error) {
	chapters, err := q.getSummaryDB()
	if summaryDBID := id - 1; len(chapters) >= summaryDBID {
		return chapters[summaryDBID], nil
	}

	var chapter struct {
		Summary ChapterSummary `json:"chapter"`
	}
	err = q.httpClient.Get(fmt.Sprintf("/chapters/%d", id)).
		Success(httpc.StatusOK()).
		DecodeJSON(&chapter).
		Do(ctx)
	if err != nil {
		return ChapterSummary{}, err
	}

	return chapter.Summary, nil
}

func (q *QuranService) getChapter(ctx context.Context, id int) (Chapter, error) {
	chapter, err := q.getChapterSummary(ctx, id)
	if err != nil {
		return Chapter{}, err
	}

	verses := make([]Verse, 0, chapter.VerseCount)
	var page, offset int
	for {
		var versesResp struct {
			Verses []Verse `json:"verses"`
		}
		err = q.httpClient.Get(fmt.Sprintf("/chapters/%d/verses", id)).
			QueryParam("page", strconv.Itoa(page)).
			QueryParam("offset", strconv.Itoa(offset)).
			QueryParam("limit", "50"). // 50 is max number of verses per req
			Success(httpc.StatusOK()).
			DecodeJSON(&versesResp).
			Do(ctx)
		if err != nil {
			return Chapter{}, err
		}
		verses = append(verses, versesResp.Verses...)
		if len(versesResp.Verses) < 50 {
			break
		}
		page++
		offset += len(versesResp.Verses)
	}

	return Chapter{
		ID:                  chapter.ID,
		Number:              chapter.Number,
		BismallahPre:        chapter.BismallahPre,
		RevelationOrder:     chapter.RevelationOrder,
		RevelationPlace:     chapter.RevelationPlace,
		NameArabic:          chapter.NameArabic,
		NameSimple:          chapter.NameSimple,
		NameTransliteration: chapter.NameTransliteration,
		Pages: Pages{
			Start: chapter.startPage(),
			End:   chapter.endPage(),
		},
		TranslatedName: struct {
			LanguageName string `json:"language_name"`
			Name         string `json:"name"`
		}{
			LanguageName: chapter.TranslatedName.LanguageName,
			Name:         chapter.TranslatedName.Name,
		},
		Verses: verses,
	}, nil
}

func (q *QuranService) ChaptersSummary(ctx context.Context) ([]ChapterSummary, error) {
	summaries, err := q.getSummaryDB()
	if err == nil {
		return summaries, nil
	}

	summaries, err = q.getSummaryAPI(ctx)
	if err != nil {
		return nil, err
	}

	if err := q.setSummaryDB(summaries); err != nil {
		log.Println(err)
	}

	return summaries, nil
}

func (q *QuranService) getSummaryAPI(ctx context.Context) ([]ChapterSummary, error) {
	var chapters struct {
		Chapters []ChapterSummary `json:"chapters"`
	}
	err := q.httpClient.Get("/chapters").
		Success(httpc.StatusOK()).
		DecodeJSON(&chapters).
		Do(ctx)
	return chapters.Chapters, err
}

const (
	bucketChapters = "chapters"

	keyChaptersSummary = "chapters_summary"
)

func (q *QuranService) deleteChapterDB(id int) error {
	return q.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketChapters))
		return b.Delete([]byte(strconv.Itoa(id)))
	})
}

func (q *QuranService) getChapterDB(ctx context.Context, id int) (Chapter, error) {
	var out Chapter
	err := q.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketChapters))
		return valueDecode(b.Get([]byte(strconv.Itoa(id))), &out)
	})
	return out, err
}

func (q *QuranService) setChapterDB(chapter Chapter) error {
	return q.db.Update(func(tx *bolt.Tx) error {
		buf, err := valueEncoder(chapter)
		if err != nil {
			return err
		}

		b := tx.Bucket([]byte(bucketChapters))
		return b.Put([]byte(strconv.Itoa(chapter.ID)), buf.Bytes())
	})
}

func (q *QuranService) getSummaryDB() ([]ChapterSummary, error) {
	var out []ChapterSummary
	err := q.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketChapters))
		return valueDecode(b.Get([]byte(keyChaptersSummary)), &out)
	})

	if len(out) != 114 {
		return nil, errors.New("no chapter summaries found")
	}

	return out, err
}

func (q *QuranService) setSummaryDB(chapters []ChapterSummary) error {
	return q.db.Update(func(tx *bolt.Tx) error {
		buf, err := valueEncoder(chapters)
		if err != nil {
			return err
		}

		b := tx.Bucket([]byte(bucketChapters))
		return b.Put([]byte(keyChaptersSummary), buf.Bytes())
	})
}

func valueDecode(b []byte, v interface{}) error {
	buf := bytes.NewBuffer(b)

	if err := gob.NewDecoder(buf).Decode(v); err != nil {
		return err
	}

	return nil
}

func valueEncoder(v interface{}) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(v); err != nil {
		return nil, err
	}
	return &buf, nil
}

func (q *QuranService) initDB() error {
	buckets := []string{bucketChapters}
	for _, bucket := range buckets {
		err := q.db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte(bucket))
			if err != nil {
				return fmt.Errorf("create bucket: %s", err)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
