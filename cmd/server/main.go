package main

import (
	"context"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/joho/godotenv"

	"journalflow/internal/ai"
	appconfig "journalflow/internal/config"
	appdb "journalflow/internal/db"
	"journalflow/internal/handlers"
	authmw "journalflow/internal/middleware"
	"journalflow/internal/session"
)

func main() {
	// .env is optional — in Docker, real env vars are injected by
	// docker-compose instead. This just makes local `go run` easier.
	_ = godotenv.Load()

	cfg := appconfig.Load()
	ctx := context.Background()

	pool, err := appdb.Connect(ctx, cfg.DBUrl)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	sessions := session.NewManager(pool)
	aiClient := ai.NewClient(cfg.BridgeURL)
	tmpl := handlers.LoadTemplates("web/templates")

	app := &handlers.App{
		DB:       pool,
		Sessions: sessions,
		AI:       aiClient,
		Tmpl:     tmpl,
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// static assets
	fs := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fs))

	// public routes
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})
	r.Get("/login", app.LoginPage)
	r.Post("/login", app.LoginSubmit)
	r.Post("/register", app.RegisterSubmit)
	r.Post("/logout", app.Logout)

	// authenticated routes
	r.Group(func(r chi.Router) {
		r.Use(authmw.RequireAuth(sessions))

		r.Get("/dashboard", app.Dashboard)

		r.Get("/journal", app.JournalList)
		r.Get("/journal/new", app.JournalNewPage)
		r.Post("/journal", app.JournalCreate)
		r.Get("/journal/{id}", app.JournalView)
		r.Post("/journal/{id}/reflect", app.JournalReflect)
		r.Post("/journal/{id}/discuss", app.JournalDiscuss)
		r.Post("/journal/{id}/complete", app.JournalComplete)

		r.Get("/admin", app.AdminPage)
		r.Post("/admin/name", app.AdminUpdateName)
		r.Post("/admin/password", app.AdminUpdatePassword)
	})

	log.Printf("JournalFlow listening on :%s", cfg.AppPort)
	if err := http.ListenAndServe(":"+cfg.AppPort, r); err != nil {
		log.Fatal(err)
	}
}
