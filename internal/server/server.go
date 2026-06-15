package server

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"coding-blitz-02/garethrader/internal/middleware"
	"coding-blitz-02/garethrader/internal/utils"
)

var flagStore *FlagStore

func Run() {
	if err := Init(); err != nil {
		utils.LogFatalf("failed to initialize server configuration: %v", err)
	}

	var err error
	flagStore, err = NewFlagStore(databaseURL)
	if err != nil {
		utils.LogFatalf("failed to initialize feature flag store: %v", err)
	}

	stack := middleware.CreateStack(
		middleware.Validate,
		middleware.Logging,
	)

	probeRouter := http.NewServeMux()
	RegisterProbeHandlers(probeRouter)

	apiRouter := http.NewServeMux()
	RegisterAPIHandlers(apiRouter)

	// Only run middleware on configured HTTP routes
	probeRouter.Handle("/", stack(apiRouter))

	server := &http.Server{
		Addr:    fmt.Sprintf(":%s", serverPort),
		Handler: probeRouter,
	}

	if certFilePath != "" && keyFilePath != "" {
		cert, err := tls.LoadX509KeyPair(certFilePath, keyFilePath)
		if err != nil {
			utils.LogFatalf("failed to load x509 key pair: %v", err)
		}
		server.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		utils.LogInfo(fmt.Sprintf("Starting HTTPS server on port: %s", serverPort))
		err = server.ListenAndServeTLS("", "")
	} else {
		utils.LogInfo(fmt.Sprintf("Starting HTTP server on port: %s", serverPort))
		err = server.ListenAndServe()
	}

	if err != nil {
		utils.LogFatal("server error:", err)
	}
}
