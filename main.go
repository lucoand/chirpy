package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/lucoand/chirpy/internal/auth"
	"github.com/lucoand/chirpy/internal/database"
)

var profanities = []string{"kerfuffle", "sharbert", "fornax"}
var port = ":8080"
var censor string = "****"

type apiConfig struct {
	jwtSecret      string
	fileserverHits atomic.Int32
	dbQueries      *database.Queries
	platform       string
	polkaKey       string
}

type chirpJSON struct {
	Body      string    `json:"body"`
	ID        uuid.UUID `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	UserID    uuid.UUID `json:"user_id"`
}

type userJSON struct {
	ID          uuid.UUID `json:"id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Email       string    `json:"email"`
	IsChirpyRed bool      `json:"is_chirpy_red"`
}

func chirpToJSON(c database.Chirp) chirpJSON {
	j := chirpJSON{}
	j.Body = c.Body
	j.ID = c.ID
	j.CreatedAt = c.CreatedAt
	j.UpdatedAt = c.UpdatedAt
	j.UserID = c.UserID
	return j
}

func userToJSON(u database.User) userJSON {
	j := userJSON{
		ID:          u.ID,
		CreatedAt:   u.CreatedAt,
		UpdatedAt:   u.UpdatedAt,
		Email:       u.Email,
		IsChirpyRed: u.IsChirpyRed,
	}
	return j
}

func filterProfanities(body string) string {
	// account for newline chars
	lines := strings.Split(body, "\n")

	// iterate over each line
	for i, line := range lines {
		// split each line into component words
		words := strings.Split(line, " ")

		// iterate over each word in the line
		for j, word := range words {

			// account for uppercase profanities
			lower := strings.ToLower(word)
			// check each word against the slice of profanities
			for _, profanity := range profanities {
				// if it matches, censor it
				if lower == profanity {
					words[j] = censor
					break
				}
			}
		}
		// join the words back into the line
		lines[i] = strings.Join(words, " ")
	}
	// join the lines back into one string
	body = strings.Join(lines, "\n")
	return body
}

func (cfg *apiConfig) middlewareMetricsInc(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cfg.fileserverHits.Add(1)
		next.ServeHTTP(w, r)
	})
}

func newApiConfig(dbQueries *database.Queries, platform string, secret string, polkaKey string) *apiConfig {
	var cfg apiConfig
	cfg.fileserverHits.Store(0)
	cfg.dbQueries = dbQueries
	cfg.platform = platform
	cfg.jwtSecret = secret
	cfg.polkaKey = polkaKey
	return &cfg
}

func (cfg *apiConfig) handleMetrics(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "<html><body><h1>Welcome, Chirpy Admin</h1><p>Chirpy has been visited %d times!</p></body></html>", cfg.fileserverHits.Load())
}

func (cfg *apiConfig) handleReset(w http.ResponseWriter, r *http.Request) {
	if cfg.platform != "dev" {
		w.WriteHeader(403)
		return
	}

	err := cfg.dbQueries.DeleteUsers(r.Context())
	if err != nil {
		log.Printf("Error deleting users: %s", err)
		w.WriteHeader(500)
		return
	}

	cfg.fileserverHits.Store(0)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, "Reset fileserverHits.\nDeleted users.\n")
}

func (cfg *apiConfig) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		log.Printf("Couldn't generate hash from password: %s", err)
		w.WriteHeader(500)
		return
	}
	query := database.CreateUserParams{
		Email:          params.Email,
		HashedPassword: hashedPassword,
	}
	user, err := cfg.dbQueries.CreateUser(r.Context(), query)
	if err != nil {
		log.Printf("Error creating user: %s", err)
		w.WriteHeader(500)
		return
	}

	uJSON := userToJSON(user)

	dat, err := json.Marshal(uJSON)
	if err != nil {
		log.Printf("Error marshaling json: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	w.Write(dat)
}

func (cfg *apiConfig) handleValidateChirp(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Body string `json:"body"`
		// UserID uuid.UUID `json:"user_id"`
	}

	type errorJson struct {
		Error string `json:"error"`
	}

	token, err := auth.GetBearerToken(r.Header)
	if err != nil {
		log.Printf("Error getting Bearer Token: %s", err)
		w.WriteHeader(401)
		return
	}
	userID, err := auth.ValidateJWT(token, cfg.jwtSecret)
	if err != nil {
		log.Printf("Error validating token: %s", err)
		w.WriteHeader(401)
		return
	}

	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	if len(params.Body) > 140 {
		resp := errorJson{
			Error: "Chirp is too long",
		}
		dat, err := json.Marshal(resp)
		if err != nil {
			log.Printf("Error marshalling JSON: %s", err)
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write(dat)
		return
	}

	cleanedBody := filterProfanities(params.Body)
	var query database.CreateChirpParams
	query.Body = cleanedBody
	query.UserID = userID

	result, err := cfg.dbQueries.CreateChirp(r.Context(), query)
	if err != nil {
		log.Printf("Error creating Chirp: %s", err)
		w.WriteHeader(500)
		return
	}

	resp := chirpJSON{
		Body:      result.Body,
		ID:        result.ID,
		CreatedAt: result.CreatedAt,
		UpdatedAt: result.UpdatedAt,
		UserID:    result.UserID,
	}

	dat, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshalling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(201)
	w.Write(dat)
}

func (cfg *apiConfig) handleGetChirps(w http.ResponseWriter, r *http.Request) {
	authorID := r.URL.Query().Get("author_id")
	chirps := []database.Chirp{}
	if authorID == "" {
		allChirps, err := cfg.dbQueries.GetChirps(r.Context())
		if err != nil {
			log.Printf("Error retrieving chirps: %s", err)
			w.WriteHeader(500)
			return
		}
		chirps = append(chirps, allChirps...)
	} else {
		authorUUID, err := uuid.Parse(authorID)
		if err != nil {
			log.Printf("Error parsing author_id: %s", err)
			w.WriteHeader(500)
			return
		}
		authorChirps, err := cfg.dbQueries.GetChirpsByUserID(r.Context(), authorUUID)
		if errors.Is(err, sql.ErrNoRows) {
			w.WriteHeader(404)
			return
		} else if err != nil {
			log.Printf("Error retrieving chirps: %s", err)
			w.WriteHeader(500)
			return
		}
		chirps = append(chirps, authorChirps...)
	}
	sortMethod := r.URL.Query().Get("sort")
	if sortMethod == "desc" {
		sort.Slice(chirps, func(i, j int) bool {
			return chirps[i].CreatedAt.After(chirps[j].CreatedAt)
		})
	}

	resp := make([]chirpJSON, len(chirps))
	for i, chirp := range chirps {
		resp[i].Body = chirp.Body
		resp[i].ID = chirp.ID
		resp[i].CreatedAt = chirp.CreatedAt
		resp[i].UpdatedAt = chirp.CreatedAt
		resp[i].UserID = chirp.UserID
	}
	dat, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshaling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(dat)
}

func handleHealthz(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "OK")
}

func (cfg *apiConfig) handleGetChirpByID(w http.ResponseWriter, r *http.Request) {
	chirpID, err := uuid.Parse(r.PathValue("chirp_id"))
	if err != nil {
		log.Printf("Error parsing chirp_id into UUID: %s", err)
		w.WriteHeader(500)
		return
	}
	chirp, err := cfg.dbQueries.GetChirp(r.Context(), chirpID)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(404)
		return
	} else if err != nil {
		log.Printf("Error retrieving chirp: %s", err)
		w.WriteHeader(500)
		return
	}
	result := chirpToJSON(chirp)
	dat, err := json.Marshal(result)
	if err != nil {
		log.Printf("Error marshaling JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(dat)
}

func (cfg *apiConfig) handleDeleteChirpByID(w http.ResponseWriter, r *http.Request) {
	accessToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		log.Printf("Error retrieving access token: %s", err)
		w.WriteHeader(401)
		return
	}
	userID, err := auth.ValidateJWT(accessToken, cfg.jwtSecret)
	if err != nil {
		log.Printf("Error validating token: %s", err)
		w.WriteHeader(401)
		return
	}
	chirpID, err := uuid.Parse(r.PathValue("chirp_id"))
	if err != nil {
		log.Printf("Error parsing chirp_id: %s", err)
		w.WriteHeader(500)
		return
	}
	chirp, err := cfg.dbQueries.GetChirp(r.Context(), chirpID)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(404)
		return
	} else if err != nil {
		log.Printf("Error retrieving chirp: %s", err)
		w.WriteHeader(500)
		return
	}
	if chirp.UserID != userID {
		w.WriteHeader(403)
		return
	}
	err = cfg.dbQueries.DeleteChirpByID(r.Context(), chirp.ID)
	if err != nil {
		log.Printf("Error deleting chirp from database: %s", err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(204)
}

func (cfg *apiConfig) handleLogin(w http.ResponseWriter, r *http.Request) {
	type parameters struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		// ExpiresIn *int64 `json:"expires_in_seconds"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err := decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	var expiresIn time.Duration
	// if params.ExpiresIn == nil || *params.ExpiresIn > 3600 {
	expiresIn = 1 * time.Hour
	// } else {
	// 	expiresIn = time.Duration(*params.ExpiresIn) * time.Second
	// }

	user, err := cfg.dbQueries.GetUserByEmail(r.Context(), params.Email)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(401)
		fmt.Fprintf(w, "Incorrect email or password\n")
		return
	}
	err = auth.CheckPasswordHash(user.HashedPassword, params.Password)
	if err != nil {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(401)
		fmt.Fprintf(w, "Incorrect email or password\n")
		return
	}
	token, err := auth.MakeJWT(user.ID, cfg.jwtSecret, expiresIn)
	if err != nil {
		log.Printf("Error generating token: %s", err)
		w.WriteHeader(500)
		return
	}

	refreshToken, err := auth.MakeRefreshToken()
	if err != nil {
		log.Printf("Error generating refresh token: %s", err)
		w.WriteHeader(500)
		return
	}

	refreshTokenDB := database.CreateRefreshTokenParams{
		Token:  refreshToken,
		UserID: user.ID,
	}

	err = cfg.dbQueries.CreateRefreshToken(r.Context(), refreshTokenDB)
	if err != nil {
		log.Printf("Error adding refresh token to database: %s", err)
		w.WriteHeader(500)
		return
	}

	type response struct {
		ID           uuid.UUID `json:"id"`
		CreatedAt    time.Time `json:"created_at"`
		UpdatedAt    time.Time `json:"updated_at"`
		Email        string    `json:"email"`
		Token        string    `json:"token"`
		RefreshToken string    `json:"refresh_token"`
		IsChirpyRed  bool      `json:"is_chirpy_red"`
	}

	resp := response{
		ID:           user.ID,
		CreatedAt:    user.CreatedAt,
		UpdatedAt:    user.UpdatedAt,
		Email:        user.Email,
		IsChirpyRed:  user.IsChirpyRed,
		Token:        token,
		RefreshToken: refreshToken,
	}

	dat, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshaling json: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(dat)
}

func (cfg *apiConfig) handleRefresh(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		log.Printf("Error getting refresh token from header: %s", err)
		w.WriteHeader(401)
		return
	}
	refreshTokenDB, err := cfg.dbQueries.GetRefreshTokenFromToken(r.Context(), refreshToken)
	if err != nil {
		log.Printf("Error retriving token from database: %s", err)
		w.WriteHeader(401)
		return
	}
	currentTime := time.Now()
	if refreshTokenDB.ExpiresAt.Before(currentTime) {
		log.Printf("Refresh token expired.")
		w.WriteHeader(401)
		return
	}
	if refreshTokenDB.RevokedAt.Valid {
		log.Printf("Refresh token revoked.")
		w.WriteHeader(401)
		return
	}
	expiresIn := 1 * time.Hour
	token, err := auth.MakeJWT(refreshTokenDB.UserID, cfg.jwtSecret, expiresIn)
	if err != nil {
		log.Printf("Error generating token: %s", err)
		w.WriteHeader(500)
		return
	}
	type response struct {
		Token string `json:"token"`
	}
	resp := response{
		Token: token,
	}
	dat, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshaling json: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(dat)
}

func (cfg *apiConfig) handleRevoke(w http.ResponseWriter, r *http.Request) {
	refreshToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		log.Printf("Error retrieving refresh token from header: %s", err)
		w.WriteHeader(401)
		return
	}
	err = cfg.dbQueries.RevokeRefreshToken(r.Context(), refreshToken)
	if err != nil {
		log.Printf("Error revoking refresh token in database: %s", err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(204)
	return
}

func (cfg *apiConfig) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	accessToken, err := auth.GetBearerToken(r.Header)
	if err != nil {
		log.Printf("Error retriving access token from header: %s", err)
		w.WriteHeader(401)
		return
	}
	userID, err := auth.ValidateJWT(accessToken, cfg.jwtSecret)
	if err != nil {
		log.Printf("Error validating access token: %s", err)
		w.WriteHeader(401)
		return
	}
	type parameters struct {
		Password string `json:"password"`
		Email    string `json:"email"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding JSON: %s", err)
		w.WriteHeader(500)
		return
	}
	hashedPassword, err := auth.HashPassword(params.Password)
	if err != nil {
		log.Printf("Error hashing password: %s", err)
		w.WriteHeader(500)
		return
	}
	query := database.UpdateUserEmailAndPasswordFromIDParams{
		ID:             userID,
		Email:          params.Email,
		HashedPassword: hashedPassword,
	}
	result, err := cfg.dbQueries.UpdateUserEmailAndPasswordFromID(r.Context(), query)
	if err != nil {
		log.Printf("Error updating user info: %s", err)
		w.WriteHeader(500)
		return
	}
	resp := userJSON{
		ID:          result.ID,
		CreatedAt:   result.CreatedAt,
		UpdatedAt:   result.UpdatedAt,
		Email:       result.Email,
		IsChirpyRed: result.IsChirpyRed,
	}
	dat, err := json.Marshal(resp)
	if err != nil {
		log.Printf("Error marshaling json: %s", err)
		w.WriteHeader(500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	w.Write(dat)
}

func (cfg *apiConfig) handlePolkaWebhooks(w http.ResponseWriter, r *http.Request) {
	apiKey, err := auth.GetAPIKey(r.Header)
	if err != nil || apiKey != cfg.polkaKey {
		w.WriteHeader(401)
		return
	}
	type parameters struct {
		Event string `json:"event"`
		Data  struct {
			UserID uuid.UUID `json:"user_id"`
		} `json:"data"`
	}
	decoder := json.NewDecoder(r.Body)
	params := parameters{}
	err = decoder.Decode(&params)
	if err != nil {
		log.Printf("Error decoding json parameters: %s", err)
		w.WriteHeader(500)
		return
	}
	if params.Event != "user.upgraded" {
		w.WriteHeader(204)
		return
	}
	user, err := cfg.dbQueries.GetUserByID(r.Context(), params.Data.UserID)
	if errors.Is(err, sql.ErrNoRows) {
		w.WriteHeader(404)
		return
	} else if err != nil {
		log.Printf("Error retrieving user from database: %s", err)
		w.WriteHeader(500)
		return
	}
	err = cfg.dbQueries.UpgradeUserToChirpyRed(r.Context(), user.ID)
	if err != nil {
		log.Printf("Error upgrading user %v to Chirpy Red: %s", user.ID, err)
		w.WriteHeader(500)
		return
	}
	w.WriteHeader(204)
	return
}

func main() {
	godotenv.Load()
	dbURL := os.Getenv("DB_URL")
	secret := os.Getenv("SECRET")
	platform := os.Getenv("PLATFORM")
	polkaKey := os.Getenv("POLKA_KEY")
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatal("ERROR: Unable to connect to database.")
	}
	dbQueries := database.New(db)
	apiCfg := newApiConfig(dbQueries, platform, secret, polkaKey)
	mux := http.NewServeMux()
	fs := http.StripPrefix("/app", http.FileServer(http.Dir(".")))
	mux.HandleFunc("POST /api/chirps", apiCfg.handleValidateChirp)
	mux.HandleFunc("GET /api/chirps", apiCfg.handleGetChirps)
	mux.HandleFunc("GET /api/chirps/{chirp_id}", apiCfg.handleGetChirpByID)
	mux.HandleFunc("DELETE /api/chirps/{chirp_id}", apiCfg.handleDeleteChirpByID)
	mux.HandleFunc("GET /api/healthz", handleHealthz)
	mux.HandleFunc("POST /api/login", apiCfg.handleLogin)
	mux.HandleFunc("POST /api/polka/webhooks", apiCfg.handlePolkaWebhooks)
	mux.HandleFunc("POST /api/refresh", apiCfg.handleRefresh)
	mux.HandleFunc("POST /api/revoke", apiCfg.handleRevoke)
	mux.HandleFunc("POST /api/users", apiCfg.handleCreateUser)
	mux.HandleFunc("PUT /api/users", apiCfg.handleUpdateUser)
	mux.HandleFunc("GET /admin/metrics", apiCfg.handleMetrics)
	mux.HandleFunc("POST /admin/reset", apiCfg.handleReset)
	mux.Handle("/app/", apiCfg.middlewareMetricsInc(fs))
	server := http.Server{}
	server.Addr = port
	server.Handler = mux
	log.Fatal(server.ListenAndServe())
}
