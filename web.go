package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Answer int

type session struct {
	User    User
	Profile TestProfile
	expiry  time.Time
	start   time.Time
	result  ResultStore
}

type DataTypes interface {
	User | Tasks | TestProfile | []TestProfile | AvailableTestProfiles | Profiles | ResultStore
}

type Message[D DataTypes] struct {
	Error struct {
		Code int    `json:"CODE"`
		Text string `json:"TEXT"`
	} `json:"ERROR"`
	Data D `json:"DATA"`
}

// map session ids to session
var sessions = map[string]session{}

var cfg Config

//go:embed static/login.html
var loginPage string

//go:embed static/signup.html
var signupPage string

//go:embed static/successfulRegistration.html
var successPage string

func processError(err error) {
	fmt.Println(err)
	os.Exit(2)
}

func newOption(id int, text string) Option {
	var o Option
	o.Id = id
	o.Text = text
	return o
}

func newCard(id int, question string, opts []Option) Card {
	var c Card
	c.Id = id
	c.Question = question
	c.Options = opts
	return c

}

func post(v any, url string) (*http.Response, error) {
	// Create json
	out, err := json.Marshal(v)
	if err != nil {
		log.Printf("post error: %v", err)
	}

	// Post json and get reponse
	resp, err := http.Post(
		url,
		"application/json",
		bytes.NewBuffer(out),
	)
	if err != nil {
		log.Printf("post error: %v", err)
	}
	return resp, err
}

func testHandler(w http.ResponseWriter, r *http.Request, sesCookie *http.Cookie, ses session) {
	switch r.Method {
	case "GET":
		var t Test
		// get user from session
		profid := strconv.Itoa(ses.Profile.Id)
		// get tasks
		tasks, _ := getTasks(profid)
		c := getCards(tasks)
		// Create test
		t = newTest(ses.User, c, ses.Profile)
		ses.start = t.Time.Start
		http.SetCookie(w, &http.Cookie{
			Name:  "testing_start",
			Value: t.Time.Start.Format(time.RFC3339),
		})
		// http.SetCookie(w, profid)
		data := TemplateData{
			Data:    t,
			Session: ses,
		}
		renderTemplate(w, "test", &data)
	case "POST":
		// Get data from form
		if err := r.ParseForm(); err != nil {
			log.Printf("ParseForm() err: %v", err)
			return
		}
		f := r.PostForm
		// Put data to Test Result
		tr, err := newTestResult(f)
		if err != nil {
			log.Printf("test error: %v", err)
		}
		testStart, _ := r.Cookie("testing_start")
		tr.Time.Start, err = time.Parse(time.RFC3339, testStart.Value)
		if err != nil {
			log.Printf("test error: %v", err)
		}
		url := baseUrl(cfg) + "/" + "tests"

		// Post test results and get response
		resp, err := post(tr, url)
		if err != nil {
			log.Printf("test error: %v", err)
		}
		// Parse stored result id
		result, _ := read[ResultStore](resp)
		// Save stored result id to session
		ses.result.Id = result.Data.Id
		// save session
		sessions[sesCookie.Value] = ses

		http.SetCookie(w, sesCookie)
		http.Redirect(w, r, "/result", http.StatusFound)
	}
}

func signInHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		fmt.Fprint(w, loginPage)
	case "POST":
		if err := r.ParseForm(); err != nil {
			log.Fatalf("ParseForm() err: %v", err)
		}

		// Read credentials from form
		c := readCreds(r.PostForm)
		if c.Email == "" || c.Password == "" {
			log.Print("Authentication error: Email and/or user are empty")
			log.Print("Redirecting to login...")
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		// Get user by from REST server
		u, err := getUser(c.Email)
		if err != nil {
			log.Print("Login error: user not found")
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		// Check password
		if c.Password != u.Auth.Password {
			log.Println("login/password not correct")
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}

		sessionToken := uuid.NewString()
		expiresAt := time.Now().Add(2 * time.Hour)

		// save data to session
		sessions[sessionToken] = session{
			User:   u,
			expiry: expiresAt,
		}
		cookie := http.Cookie{}
		cookie.Name = "gosesid"
		cookie.Value = sessionToken
		cookie.Path = "/"
		http.SetCookie(w, &cookie)
		log.Println("Login: success. Redirecting...")
		http.Redirect(w, r, "/profiles", http.StatusFound)
	}
}

// TODO: Reimplement this function
func signUpHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		fmt.Fprint(w, signupPage)
	case "POST":
		if err := r.ParseForm(); err != nil {
			log.Printf("ParseForm() err: %v", err)
			return
		}

		u := newUser(
			r.Form["name"][0],
			r.Form["middlename"][0],
			r.Form["surname"][0],
			r.FormValue("sex"),
			r.Form["email"][0],
			r.Form["password"][0])
		// fmt.Fprintf(w, "%v", u)
		// Send registration data to REST server
		url := baseUrl(cfg) + "/persons"
		resp, _ := post(u, url)
		// Parse response with user data from REST server
		read[User](resp)
		http.Redirect(w, r, "/success", http.StatusFound)

	}
}

func profilesHandler(w http.ResponseWriter, r *http.Request, sesCookie *http.Cookie, ses session) {
	switch r.Method {
	case http.MethodGet:
		var profiles Profiles
		// get profiles
		profiles, _ = getProfiles()
		// Create template data
		data := TemplateData{Data: profiles, Session: ses}

		// renderTemplate(w, "profiles", &profiles)
		renderTemplate(w, "profiles", &data)
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			log.Printf("ParseForm() err: %v", err)
			return
		}
		log.Printf("form: %+v\n", r.Form)

		pids := r.FormValue("TASK_PROFILE_ID")
		pid, _ := strconv.Atoi(pids)
		ptext := r.FormValue("TASK_PROFILE_TEXT_" + pids)
		profile := TestProfile{
			Id:   pid,
			Text: ptext,
		}

		ses.Profile = profile
		// Save session
		sessions[sesCookie.Value] = ses
		http.Redirect(w, r, "/test", http.StatusFound)

	}

}

func resultHandler(w http.ResponseWriter, r *http.Request, sesCookie *http.Cookie, ses session) {
	result, _ := getResult(ses.result.Id)
	// test scenario
	// result, _ := getResult(255)
	data := TemplateData{
		Data:    result,
		Session: ses,
	}
	renderTemplate(w, "result", &data)
	if result.Certified {
		c, err := cert(ses.result.Id)
		if err != nil {
			log.Print(err)
			return
		}
		log.Print(c)
		initMail()
		s := "Аттестация ЯОКБ"
		b := "Поздравляем с успешной аттестацией! Сертификат прикреплен в приложении."
		m := NewMessage(s, b)
		m.To = append(m.To, ses.User.Auth.Email)
		m.From = cfg.SMTP.User
		err = m.AttachFile(c)
		if err != nil {
			log.Print(err)
			return
		}
		sndr := NewSender()
		err = sndr.Send(m)
		if err != nil {
			log.Print(err)
			return
		}
		log.Print("Successfully emailed certificate")
	}
}

func logout(w http.ResponseWriter, r *http.Request, sesCookie *http.Cookie, ses session) {
	log.Print("Removing session...")
	delete(sessions, sesCookie.Value)
	log.Print("Removing session cookie")
	sesCookie.Expires = time.Now()
	http.SetCookie(w, sesCookie)
	// Open login page
	http.Redirect(w, r, "/login", http.StatusFound)
}

func successHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, successPage)
}

func getTasks(profileId string) (tasks Tasks, err error) {
	// Build url
	url := strings.Join([]string{
		baseUrl(cfg),
		"profiles",
		profileId,
		"tasks",
	}, "/")
	// Send request and get response
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	// Parse response to message
	m, err := read[Tasks](resp)
	if err != nil {
		return
	}
	// Save message data to tasks
	tasks = m.Data
	return
}

func getProfiles() (profiles Profiles, err error) {
	url := baseUrl(cfg) + "/profiles"
	// Send request
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	// Parse response
	m, err := read[Profiles](resp)
	if err != nil {
		return
	}
	// Save message data to profiles
	profiles = m.Data
	return
}

func getResult(id int) (result ResultStore, err error) {
	ids := strconv.Itoa(id)
	url := baseUrl(cfg) + "/tests/" + ids
	// Send request
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	// Parse response
	m, err := read[ResultStore](resp)
	if err != nil {
		return
	}
	result = m.Data
	return

}

func cert(id int) (name string, err error) {
	testID := strconv.Itoa(id)
	url := baseUrl(cfg) + "/tests/" + testID + "/cert"
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return
	}
	name, err = saveCert(data)
	return
}

func readCreds(f url.Values) Credentials {
	return Credentials{
		Email:    f["email"][0],
		Password: f["password"][0],
	}
}

func read[DT DataTypes](r *http.Response) (m Message[DT], err error) {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}
	err = json.Unmarshal(data, &m)
	if err != nil {
		return
	}
	if m.Error.Code == 0 {
		err = nil
	}
	return
}

func root(w http.ResponseWriter, r *http.Request) {
	log.Print("/ accessed. Redirecting to /login")
	http.Redirect(w, r, "/login", http.StatusFound)
}

func authenticate(fn func(http.ResponseWriter, *http.Request, *http.Cookie, session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get session cookie
		sesCookie, err := r.Cookie("gosesid")
		if err != nil {
			log.Printf("Authenticate error: %v (no cookie)", err)
			log.Print("Redirecting to login page...")
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		// Get session by id
		ses, exists := sessions[sesCookie.Value]
		if !exists {
			log.Printf("Session not exists (id: %v)", sesCookie.Value)
			log.Print("Redirecting to login page...")
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		fn(w, r, sesCookie, ses)

	}
}
