package app

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/gyuho/goling/spellcheck"
)

var (
	words   map[string]int
	ratings map[string]string
)

func init() {
	data, err := ParseCSV("./imdb-movies.csv", true)
	if err != nil {
		log.Fatal(err)
	}

	words = make(map[string]int, 1000)
	ratings = make(map[string]string, 1000)

	for _, r := range data {
		clean := strip(strings.ToLower(r.Movie))
		ratings[clean] = r.Rating
		for _, w := range strings.Split(clean, " ") {
			words[w] += 1
		}
	}
}

var client = http.Client{
	Timeout: 5 * time.Second,
}

func Ping(domain string, quit chan struct{}) {
	url := "http://" + domain
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return
	}
	resp.Body.Close()
	if resp.StatusCode == 200 {
		quit <- struct{}{}
	}
}

func GetPrompts(q string, n int) []string {
	u, err := url.Parse("http://0.0.0.0:80/q?query=something&n=4")
	if err != nil {
		log.Fatal(err)
	}
	u.Scheme = "http"
	u.Host = "pyapp:80"
	qVals := u.Query()
	qVals.Set("query", q)
	qVals.Set("n", fmt.Sprintf("%d", n))
	u.RawQuery = qVals.Encode()
	req, err := http.NewRequest("GET", u.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	type response struct {
		Items []string `json:"items"`
	}
	r := new(response)
	err = json.NewDecoder(resp.Body).Decode(r)
	if err != nil {
		log.Fatal(err)
	}
	return r.Items
}

type errMsg error

type uimodel struct {
	textInput textinput.Model
	choices   []string
	selected  map[int]struct{}
	cursor    int
	err       error
}

func initialModel() uimodel {
	ti := textinput.New()
	ti.Placeholder = "Movie.."
	ti.Focus()
	ti.CharLimit = 156
	ti.Width = 20

	return uimodel{
		textInput: ti,
		choices:   make([]string, 0),
		selected:  make(map[int]struct{}),
		cursor:    0,
		err:       nil,
	}
}

func (m uimodel) Init() tea.Cmd {
	return textinput.Blink
}

func (m uimodel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
		case tea.KeyDown:
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case tea.KeyEnter:
			m.textInput, cmd = m.textInput.Update(msg)
			m.choices = prompter(m.textInput.Value())
			return m, cmd
		case tea.KeyTab:
			m.textInput, cmd = m.textInput.Update(msg)
			m.textInput.SetValue(m.choices[m.cursor])
			m.choices = prompter(m.textInput.Value())
			return m, cmd
		}

	// We handle errors just like any other message
	case errMsg:
		m.err = msg
		return m, nil
	}

	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m uimodel) View() string {
	s := fmt.Sprintf(
		"Search for movie ([Tab] to select prompt)\n\n%s\n\n",
		m.textInput.View(),
	)
	for i, choice := range m.choices {

		// Is the cursor pointing at this choice?
		cursor := " " // no cursor
		if m.cursor == i {
			cursor = ">" // cursor!
		}

		// Is this choice selected?
		checked := " " // not selected
		if _, ok := m.selected[i]; ok {
			checked = "x" // selected!
		}

		// Render the row
		s += fmt.Sprintf("%s [%s%s]\n", cursor, checked, choice)
	}
	s += "([esc] to quit)\n"
	return s
}

type Record struct {
	Movie  string
	Rating string
}

func strip(s string) string {
	var result strings.Builder
	for i := 0; i < len(s); i++ {
		b := s[i]
		if ('a' <= b && b <= 'z') ||
			('0' <= b && b <= '9') ||
			b == ' ' {
			result.WriteByte(b)
		}
	}
	return result.String()
}

func TeaUI() {
	p := tea.NewProgram(initialModel())
	log.Println("starting tea..")
	if err := p.Start(); err != nil {
		log.Fatal(err)
	}
}

func prompter(query string) []string {
	q := strip(strings.ToLower(query))
	queryWords := strings.Split(q, " ")
	prompts := make([]string, 0, 5)
	var str1, str2 string
	for i, w := range queryWords {
		if i == len(queryWords)-1 {
			temp := spellcheck.Suggest(w, words)
			str1 += temp
			str2 += temp
		} else {
			str1 += spellcheck.Suggest(w, words) + " "
			str2 += w + " "
		}
	}
	if str2 != q {
		prompts = append(prompts, str2)
	}
	if str1 != q && str1 != str2 {
		prompts = append(prompts, str1)
	}
	mlprompts := GetPrompts(str1, cap(prompts)-len(prompts))
	sort.Slice(mlprompts, func(i, j int) bool { return ratings[mlprompts[i]] > ratings[mlprompts[j]] })
	prompts = append(prompts, mlprompts...)
	// prompts[0] --> pyapp --> cap(prompts)-len(prompts) more prompts
	return prompts
}

func ParseCSV(filepath string, rating bool) ([]Record, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	reader := csv.NewReader(file)
	reader.ReuseRecord = true
	if !rating {
		data := make([]Record, 0, 1000)
		_, err := reader.Read()
		if err != nil {
			return nil, err
		}
		for {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			data = append(data, Record{Movie: record[1]})
		}
		return data, nil
	}
	data := make([]Record, 0, 1000)
	_, err = reader.Read()
	if err != nil {
		return nil, err
	}
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		data = append(data, Record{Movie: record[1], Rating: record[8]})
	}
	return data, nil
}
