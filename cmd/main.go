package main

import (
	"bytes"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/adrg/frontmatter"
	"github.com/labstack/echo/v4"
	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting"
)

type Post struct {
	Title   string `toml:"title"`
	Slug    string `toml:"slug"`
	Content template.HTML
	Author  Author `toml:"author"`
	Loaded  bool
}

type Author struct {
	Name  string `toml:"name"`
	Email string `toml:"email"`
}
type Posts []Post
type Data struct {
	Posts Posts
}

func newData() Data {
	return Data{
		Posts: []Post{},
	}
}

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

var postsMap = make(map[string]*Post)
var postMutex = &sync.Mutex{}

func main() {
	e := echo.New()
	t := &Template{
		templates: template.Must(template.ParseGlob("views/*.html")),
	}
	files, err := os.ReadDir("posts/")
	if err != nil {
		log.Fatal(err)
	}
	posts := newData()
	fr := FileReader{}
	for _, file := range files {
		slug := strings.TrimSuffix(file.Name(), ".md")
		postMarkdown, err := fr.Read(slug)
		if err != nil {
			log.Fatal("Error reading post:%v", err)
		}
		var post Post
		post.Slug = slug
		_, err = frontmatter.Parse(strings.NewReader(postMarkdown), &post)
		if err != nil {
			log.Fatal("Error reading metadata:%v", err)
		}
		postMutex.Lock()
		postsMap[slug] = &post
		postMutex.Unlock()
		posts.Posts = append(posts.Posts, post)
	}

	e.Renderer = t

	// Render all posts
	e.GET("/", func(c echo.Context) error {
		return c.Render(200, "index.html", posts)
	})

	e.GET("/posts/:slug", func(c echo.Context) error {
		slug := c.Param("slug")
		postHandler := PostHandler(FileReader{})
		return postHandler(c, slug)
	})

	e.Logger.Fatal(e.Start(":42069"))
}

type SlugReader interface {
	Read(slug string) (string, error)
}

type FileReader struct{}

func (fsr FileReader) Read(slug string) (string, error) {
	file, err := os.Open("posts/" + slug + ".md")
	if err != nil {
		return "", err
	}
	defer file.Close()
	b, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func PostHandler(sl SlugReader) func(c echo.Context, slug string) error {
	return func(c echo.Context, slug string) error {
		postMutex.Lock()
		post, exists := postsMap[slug]
		if !exists {
			return c.String(http.StatusNotFound, "Post not found")
		}
		if !post.Loaded {
			postMarkdown, err := sl.Read(post.Slug)
			contentStart := strings.LastIndex(postMarkdown, "+++\n") + len("+++\n")
			rest := postMarkdown[contentStart:]
			mdRenderer := goldmark.New(
				goldmark.WithExtensions(
					highlighting.NewHighlighting(
						highlighting.WithStyle("dracula"),
					),
				),
			)
			var buf bytes.Buffer
			err = mdRenderer.Convert([]byte(rest), &buf)
			if err != nil {
				c.String(http.StatusInternalServerError, "Could not render md")
			}
			postMutex.Lock()
			post.Content = template.HTML(buf.String())
			post.Loaded = true
			postMutex.Unlock()
		}
		return c.Render(http.StatusOK, "post.html", post)
	}
}
