package main

import (
	"html/template"
	"github.com/russross/blackfriday/v2"
	"log"
	"net/http"
	"os"
	"regexp"
	"errors"
	"strings"
	"io"
	"encoding/json"
	"bytes"
	"fmt"
	"io/ioutil"
	"os/exec"
	"database/sql"
	_ "github.com/lib/pq"
)

var validPath = regexp.MustCompile("^/(edit|save|view|delete|create|upload|RMSD|review|signin|signup|register|movie)/?([a-zA-Z0-9/]*)$")
const uploadPath = "./uploads"
const imagePath = "./images"

const (
	host = "localhost"
	port = 8080
	user = "koyano"
	password = "hoge"
	dbname = "users"
)

type Page struct {
	Title string
	MarkDown []byte
	Body  []byte
	ImageExists bool
}

type ViewData struct {
	Title       string
	Body        template.HTML
	RecentPages []string
	ImageExists bool
}

func safeHTML(input string)template.HTML{
	return template.HTML(input)
}

func (p *Page) save() error {
	filename := p.Title + ".txt"
	delimiter := "---ENDOFMARKDOWN---"
	content := append(p.MarkDown, []byte(delimiter)...)
	content = append(content, p.Body...)
	return os.WriteFile(filename, content, 0600)
}

func loadPage(title string) (*Page, error) {
	filename := title + ".txt"
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(content, []byte("---ENDOFMARKDOWN---"))
	if len(parts)!=2{
		return &Page{Title: title, Body: parts[0]}, nil
	}
	return &Page{Title: title, MarkDown: parts[0], Body: parts[1]}, nil
}

func renderTemplate(w http.ResponseWriter, tmpl string, p *Page) {
	t, err := template.ParseFiles("templates/" + tmpl + ".html")
	if err != nil{
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = t.Execute(w, p)
	if err != nil{
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func getSavedPages()([]string, error){
	files, err := os.ReadDir(".")
	if err != nil{
		return nil, err
	}
	var titles []string
	for _, f := range files{
		if strings.HasSuffix(f.Name(), ".txt"){
			titles = append(titles, strings.TrimSuffix(f.Name(), ".txt"))
		}
	}
	return titles, nil
}

func viewHandler(w http.ResponseWriter, r *http.Request, title string) {	
	if title == "" {
		titles, err := getSavedPages()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		tmpl, err := template.ParseFiles("templates/pages.html")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		err = tmpl.Execute(w, titles)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	} else {
		title = strings.ReplaceAll(title, "/", "-")
		p, err := loadPage(title)
		if err != nil {
			http.Redirect(w, r, "/edit/"+title, http.StatusFound)
			return
		}
		recentPages, err := getSavedPages()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		p.ImageExists = imageExists(p.Title)  // この行を保持
		htmlBody := template.HTML(blackfriday.Run(p.Body))
		data := ViewData{
			Title:       p.Title,
			Body:        htmlBody,
			RecentPages: recentPages,
			ImageExists: p.ImageExists, // p.ImageExists からの情報を ViewData にも渡す
		}
		renderTemplateWithData(w, "view", data)
	}
}

func htmlToPDF(html string, pdfPath string)error{
	tmpFile, err := ioutil.TempFile("", "temp-html-*.html")
	if err != nil{
		return err
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Write([]byte(html))
	tmpFile.Close()
	cmd := exec.Command("hkhtmltopdf", tmpFile.Name(), pdfPath)
	return cmd.Run()
}

func renderTemplateWithData(w http.ResponseWriter, tmplName string, data ViewData) {
	tmpl, err := template.ParseFiles("templates/" + tmplName + ".html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func editHandler(w http.ResponseWriter, r *http.Request, title string) {
	p, err := loadPage(title)
	if err != nil {
		p = &Page{Title: title}
	}
	renderTemplate(w, "edit", p)
}

func saveHandler(w http.ResponseWriter, r *http.Request, title string){
	body := r.FormValue("body")
	htmlOutput := blackfriday.Run([]byte(body))
	p := &Page{Title: title, MarkDown: []byte(body), Body: htmlOutput}
	err := p.save()
	if err != nil{
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	file, _, err := r.FormFile("uploadfile")
	if err == nil{
		defer file.Close()
		if _, err := os.Stat(uploadPath); os.IsNotExist(err){
			os.Mkdir(uploadPath, 0700)
		}
		f, err := os.Create(uploadPath + "/" + title + ".png")
		if err != nil{
			http.Error(w, "Unable to save image", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		_, err = io.Copy(f, file)
		if err != nil{
			http.Error(w, "Failed to save image", http.StatusInternalServerError)
			return
		}
	}
	http.Redirect(w, r, "/view/"+title, http.StatusFound)
}

func getTitle(w http.ResponseWriter, r *http.Request)(string, error){
	m := validPath.FindStringSubmatch(r.URL.Path)
	if m == nil{
		http.NotFound(w, r)
		return "", errors.New("invalid Page Title")
	}
	return m[2], nil
}

func makeHandler(fn func (http.ResponseWriter, *http.Request, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request){
		m := validPath.FindStringSubmatch(r.URL.Path)
		if m == nil{
			http.NotFound(w, r)
			return
		}
		fn(w, r, m[2])
	}
}

func cssHandler(w http.ResponseWriter, r *http.Request){
	http.ServeFile(w, r, "templates/view.css")
}

func deleteHandler(w http.ResponseWriter, r *http.Request, title string){
	err := deletePage(title)
	if err != nil{
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/view/", http.StatusFound)
}

func deletePage(title string)error{
	filename := title + ".txt"
	return os.Remove(filename)
}

func createHandler(w http.ResponseWriter, r *http.Request){
	title := r.FormValue("title")
	if title == ""{
		http.Error(w, "Title cannot be empty", http.StatusBadRequest)
		return
	}
	safeTitle := strings.ReplaceAll(title, "/", "-")
	_, err := loadPage(safeTitle)
	if err == nil{
		http.Error(w, "Page already exists", http.StatusConflict)
		return
	}
	http.Redirect(w, r, "/edit/"+title, http.StatusFound)
}

func uploadHandler(w http.ResponseWriter, r *http.Request){
	if r.Method == "POST"{
		file, _, err := r.FormFile("uploadfile")
		if err != nil{
			http.Error(w, "Unable to upload file", http.StatusInternalServerError)
			return
		}
		defer file.Close()
		if _, err := os.Stat(uploadPath); os.IsNotExist(err){
			os.Mkdir(uploadPath, 0700)
		}
		title, err := getTitle(w, r)
		if err != nil{
			http.Error(w, "Invalid Page Title", http.StatusBadRequest)
			return
		}	
		f, err := os.Create(uploadPath + "/" + title+".png")
		if err != nil{
			http.Error(w, "Unable to save file", http.StatusInternalServerError)
			return
		}
		defer f.Close()
		_, err = io.Copy(f, file)
		if err != nil{
			http.Error(w, "Failed to save file", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/edit/"+title, http.StatusFound)
	}else{
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

func imageHandler(w http.ResponseWriter, r *http.Request){
	http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadPath))).ServeHTTP(w, r)
}

func imageExists(title string)bool{
	if _, err := os.Stat(uploadPath + "/" + title + ".png"); os.IsNotExist(err){
		return false
	}
	return true
}

func rmsdHandler(w http.ResponseWriter, r *http.Request){
    var rmsdScore string
    if r.Method == "POST"{
        pdbID1 := r.FormValue("pdbID1")
        pdbID2 := r.FormValue("pdbID2")
        payload := map[string]string{
            "pdbID1": pdbID1,
            "pdbID2": pdbID2,
        }
        jsonPayload, err := json.Marshal(payload)
        if err != nil{
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        resp, err:= http.Post("http://127.0.0.1:5000/calculate_rmsd", "application/json", bytes.NewBuffer(jsonPayload))
        if err != nil{
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer resp.Body.Close()
        var result map[string]float64
        err = json.NewDecoder(resp.Body).Decode(&result)
        if err != nil{
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        rmsdValue := result["rmsd"]
        rmsdScore = fmt.Sprintf("RMSD Score: %f", rmsdValue)
    }
    t, err := template.ParseFiles("templates/rmsd.html")
    if err != nil{
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    data := map[string]string{
        "RMSDScore": rmsdScore,
    }
    err = t.Execute(w, data)
    if err != nil{
        http.Error(w, err.Error(), http.StatusInternalServerError)
    }
}

func reviewHandler(w http.ResponseWriter, r *http.Request){
    var reviewedResult string
    if r.Method == "POST"{
        sentences := r.FormValue("sentences")
		if sentences == ""{
			log.Println("Sentences are empty!")
			http.Error(w, "Sentences cannot be empty", http.StatusBadRequest)
			return
		}
		log.Printf("Received sentences: %s", sentences)
        payload := map[string]string{
            "sentences": sentences,
        }
        jsonPayload, err := json.Marshal(payload)
        if err != nil{
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        resp, err:= http.Post("http://127.0.0.1:5000/review_sentence", "application/json", bytes.NewBuffer(jsonPayload))
        if err != nil{
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer resp.Body.Close()
        var result map[string]string
        err = json.NewDecoder(resp.Body).Decode(&result)
        if err != nil{
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        res := result["result"]
		reviewedResult = string(res)
    }
    t, err := template.ParseFiles("templates/review.html")
    if err != nil{
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    data := map[string]string{
        "ReviewedResult": reviewedResult,
    }
    err = t.Execute(w, data)
    if err != nil{
        http.Error(w, err.Error(), http.StatusInternalServerError)
    }
}

func movieHandler(w http.ResponseWriter, r *http.Request){
    var movieResult string
    if r.Method == "POST"{
        userid := r.FormValue("userid")
		if userid == ""{
			log.Println("Movies are empty!")
			http.Error(w, "Movies cannot be empty", http.StatusBadRequest)
			return
		}
		log.Printf("Received userid: %s", userid)
        payload := map[string]string{
            "userid": userid,
        }
        jsonPayload, err := json.Marshal(payload)
        if err != nil{
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        resp, err:= http.Post("http://127.0.0.1:5000/movie_rec", "application/json", bytes.NewBuffer(jsonPayload))
        if err != nil{
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        defer resp.Body.Close()
        var result map[string]string
        err = json.NewDecoder(resp.Body).Decode(&result)
        if err != nil{
            http.Error(w, err.Error(), http.StatusInternalServerError)
            return
        }
        res := result["result"]
		movieResult = string(res)
    }
    t, err := template.ParseFiles("templates/movie.html")
    if err != nil{
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    data := map[string]string{
        "MovieResult": movieResult,
    }
    err = t.Execute(w, data)
    if err != nil{
        http.Error(w, err.Error(), http.StatusInternalServerError)
    }
}

func signupHandler(w http.ResponseWriter, r *http.Request){
	if r.Method == "GET"{
		t, err := template.ParseFiles("templates/signup.html")
		if err != nil{
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		t.Execute(w, nil)
	}else if r.Method == "POST"{
		username := r.FormValue("username")
		password := r.FormValue("password")
		fmt.Printf("Received signup request for username: %\n", username)
		fmt.Printf("Password: %s\n", password)
		http.Redirect(w, r, "/view/", http.StatusFound)
	}
}

func signinHandler(w http.ResponseWriter, r *http.Request){
	if r.Method == "GET"{
		t, err := template.ParseFiles("templates/signin.html")
		if err != nil{
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		t.Execute(w, nil)
	}else if r.Method == "POST"{
		username := r.FormValue("username")
		password := r.FormValue("password")
		fmt.Printf("Received signin request for username: %\n", username)
		fmt.Printf("Password: %s\n", password)
		http.Redirect(w, r, "/view/", http.StatusFound)
	}
}

func registerHandler(w http.ResponseWriter, r *http.Request){
	r.ParseForm()
	username := r.FormValue("username")
	pwd := r.FormValue("password")
	if err := storeUser(username, pwd); err != nil{
		http.Error(w, "Failed to store user", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "User registered successfully!")
}

func storeUser(username, pwd string) error {
	psqlInfo := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable", host, port, user, password, dbname)
	db, err := sql.Open("postgres", psqlInfo)
	if err != nil{
		return err
	}
	defer db.Close()
	_, err = db.Exec("INSERT INTO users (username, password) VALUES ($1 $2)", username, pwd)
	return err
}

func main() {
	http.HandleFunc("/view/", makeHandler(viewHandler))
	http.HandleFunc("/edit/", makeHandler(editHandler))
	http.HandleFunc("/save/", makeHandler(saveHandler))
	http.HandleFunc("/templates/view.css", cssHandler)
	http.HandleFunc("/delete/", makeHandler(deleteHandler))
	http.HandleFunc("/create", createHandler)
	http.HandleFunc("/upload", uploadHandler)
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir(uploadPath))))
	http.Handle("/images/", http.StripPrefix("/images/", http.FileServer(http.Dir(imagePath))))
	http.HandleFunc("/RMSD", rmsdHandler)
	http.HandleFunc("/review", reviewHandler)
	http.HandleFunc("/signup", signupHandler)
	http.HandleFunc("/signin", signinHandler)
	http.HandleFunc("/register", registerHandler)
	http.HandleFunc("/movie", registerHandler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}
