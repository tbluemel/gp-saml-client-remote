package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
)

type Server struct {
	mutex sync.Mutex
	apiMutex sync.Mutex
	openconnect *exec.Cmd
	stdoutChan chan struct{}
	stderrChan chan struct{}
	event chan struct{}
	eventNotified bool
	samlUrl string
	lastError string
	connected bool
}

type ConnectInfo struct {
	User string `json:"user"`
	Password *string `json:"password"`
	Saml *string `json:"saml"`
	Gateway string `json:"gw"`
	GatewayDomain string `json:"gwdom"`
	AddtlOpenConnectArgs *[]string `json:"oc-addtl-args"`
}

func (ci *ConnectInfo) hasValidValues() bool {
	if ci.User == "" || ci.Gateway == "" || ci.GatewayDomain == "" {
		return false
	}
	if (ci.Password == nil && ci.Saml == nil) || (ci.Password != nil && ci.Saml != nil) {
		return false
	}
	if (ci.Password != nil && *ci.Password == "") || (ci.Saml != nil && *ci.Saml == "") {
		return false
	}
	return true
}

func (s *Server) connect(info *ConnectInfo) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.connectLocked(info)
}

func (s *Server) connectLocked(info *ConnectInfo) error {
	s.disconnectLocked()

	s.event = make(chan struct{})

	stdinBuffer := bytes.Buffer{}
	if info.Saml != nil {
		stdinBuffer.Write([]byte(*info.Saml))
	} else if info.Password != nil {
		stdinBuffer.Write([]byte(*info.Password))
	}
	args := []string{"--non-inter", "--passwd-on-stdin", "--protocol=gp", fmt.Sprintf("--authgroup=%v", info.Gateway)}
	if info.Saml != nil {
		args = append(args, "--usergroup=gateway:prelogin-cookie")
	} else {
		args = append(args, "--usergroup=gateway")
	}
	if info.AddtlOpenConnectArgs != nil {
		args = append(args, *info.AddtlOpenConnectArgs...)
	}
	args = append(args, fmt.Sprintf("--user=%v", info.User), info.GatewayDomain)
	log.Println("Starting openconnect with args:", args)
	s.openconnect = exec.Command("openconnect", args...)
	s.openconnect.Stdin = &stdinBuffer

	s.stdoutChan = make(chan struct{})
	stdoutPipe, err := s.openconnect.StdoutPipe()
	if err != nil {
		log.Println("connect: Error setting up stdout reader for openconnect: %v", err)
		return err
	}
	stderrPipe, err := s.openconnect.StderrPipe()
	if err != nil {
		log.Println("connect: Error setting up stderr reader for openconnect: %v", err)
		return err
	}

	openconnectOutputHandler := func(name string, pipe io.ReadCloser, c chan struct{}, lineHandler func(string), cleanupHandler func()) {
		go func() {
			defer close(c)
			defer cleanupHandler()
			log.Printf("Start %v from openconnect", name)
			scanner := bufio.NewScanner(pipe)
			for scanner.Scan() {
				line := scanner.Text()
				log.Printf("openconnect[%v]: %v", name, line)
				lineHandler(line)
			}
			log.Printf("Done reading %v from openconnect", name)
		}()
	}

	s.stderrChan = make(chan struct{})

	rSaml := regexp.MustCompile("^SAML REDIRECT .*(?P<url>https?://.*)")
	rConnected := regexp.MustCompile("(?P<reason>session established|tunnel connected)")
	rSessionExp := regexp.MustCompile("^Session authentication will expire at (?P<exp>.*)")
	rGatewaysAvailable := regexp.MustCompile("^(?P<count>[0-9]+)gateway servers available")
	evt := s.event
	stdoutC := s.stdoutChan
	openconnectOutputHandler("stdout", stdoutPipe, stdoutC, func(line string) {
		if m := rSaml.FindStringSubmatch(line); m != nil {
			samlUrl := m[rSaml.SubexpIndex("url")]

			s.mutex.Lock()
			if stdoutC == s.stdoutChan {
				log.Printf("Found SAML URL: %v", samlUrl)
				s.samlUrl = samlUrl
				if !s.eventNotified {
					s.eventNotified = true
					evt <- struct{}{}
				}
			}
			s.mutex.Unlock()
		} else if m := rConnected.FindStringSubmatch(line); m != nil {
			reason := m[rConnected.SubexpIndex("reason")]

			s.mutex.Lock()
			if stdoutC == s.stdoutChan {
				log.Printf("Connected: %v", reason)
				s.connected = true
				if !s.eventNotified {
					s.eventNotified = true
					evt <- struct{}{}
				}
			}
			s.mutex.Unlock()
		} else if m := rSessionExp.FindStringSubmatch(line); m != nil {
			exp := m[rSessionExp.SubexpIndex("exp")]
			tt, err := time.ParseInLocation(time.ANSIC, exp, time.Local) // openconnect prints the time as local time
			if err != nil {
			   fmt.Println("Could not parse session expiration:", err)
			} else {
				log.Println("Session expiration:", tt)
			}
		} else if m := rGatewaysAvailable.FindStringSubmatch(line); m != nil {
			count := m[rGatewaysAvailable.SubexpIndex("count")]

			log.Println("Number of gateways available:", count)
		}
	}, func() {
		log.Println("Done reading from stdout, closing event")
		close(evt)
	})

	stderrC := s.stderrChan
	openconnectOutputHandler("stderr", stderrPipe, stderrC, func(line string) {
		if strings.HasPrefix(line, "Failed") {
			s.mutex.Lock()
			if stderrC == s.stderrChan {
				s.lastError = line
			}
			s.mutex.Unlock()
		}
	}, func() {
		log.Println("Done reading from stderr")
	})

	log.Println("connect: Starting openconnect")
	if err = s.openconnect.Start(); err != nil {
		log.Println("connect: Error starting openconnect:", err)
		return err
	}

	oc := s.openconnect
	go func() {
		s.mutex.Lock()
		defer s.mutex.Unlock()

		s.mutex.Unlock()
		log.Println("connect: openconnect is running")

		oc.Wait()

		s.mutex.Lock()
		s.connected = false
		log.Println("connect: openconnect did terminate")
	}()
	log.Println("connect: Started openconnect")


	return nil
}

func (s *Server) disconnectLocked() {
	if s.openconnect != nil {
		oc := s.openconnect
		s.openconnect = nil

		log.Println("disconnect: Sending SIGTERM to openconnect")
		oc.Process.Signal(syscall.SIGTERM)
		log.Println("disconnect: Waiting for openconnect to terminate")
		s.mutex.Unlock()
		oc.Wait()
		s.mutex.Lock()
		log.Println("disconnect: openconnect terminated")
	}
	
	waitForChanAndDrop := func(ch *chan struct{}) {
		if *ch != nil {
			c := *ch
			*ch = nil

			s.mutex.Unlock()
			<-c
			s.mutex.Lock()
		}
	}

	waitForChanAndDrop(&s.stdoutChan)
	waitForChanAndDrop(&s.stderrChan)
	waitForChanAndDrop(&s.event)

	s.samlUrl = ""
	s.lastError = ""
	s.connected = false
	s.eventNotified = false
}

func (s *Server) disconnect() {
	s.mutex.Lock()
	defer s.mutex.Unlock()
}

func (s *Server) statusLocked() string {
	if s.openconnect != nil && s.connected {
		return "connected"
	}
	return "disconnected"
}

func (s *Server) status() string {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.statusLocked()
}

func (s *Server) Shutdown() {
	log.Println("Shutdown requested")
	defer log.Println("Shutdown complete")

	s.apiMutex.Lock()
	defer s.apiMutex.Unlock()

	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.disconnectLocked()
}

func setupRouter(s *Server) *gin.Engine {
	r := gin.Default()

	// Ping test
	r.GET("/status", func(c *gin.Context) {
		s.apiMutex.Lock()
		defer s.apiMutex.Unlock()

		c.String(http.StatusOK, s.status())
	})

	r.POST("/connect", func(c *gin.Context) {
		s.apiMutex.Lock()
		defer s.apiMutex.Unlock()

		s.mutex.Lock()
		defer s.mutex.Unlock()
		var info ConnectInfo
		c.BindJSON(&info)
		if !info.hasValidValues() {
			log.Println("connect: Missing or invalid information")
			c.String(http.StatusBadRequest, "Missing or invalid information")
			return
		}
		if err := s.connectLocked(&info); err != nil {
			c.String(http.StatusBadGateway, err.Error())
			return
		}

		log.Println("Waiting for event...")
		evt := s.event
		s.mutex.Unlock()

		<- evt

		log.Println("Done waiting for event")
		s.mutex.Lock()

		if s.samlUrl != "" {
			samlUrl := s.samlUrl
			s.samlUrl = ""

			log.Println("/connect did get a saml url:", samlUrl)
			c.String(http.StatusOK, fmt.Sprintf("SAML-URL:%v", samlUrl))
		} else {
			log.Println("/connect did not get a saml url, return:", s.statusLocked())
			c.String(http.StatusOK, s.statusLocked())
		}
	})

	r.POST("/disconnect", func(c *gin.Context) {
		s.apiMutex.Lock()
		defer s.apiMutex.Unlock()

		s.mutex.Lock()
		defer s.mutex.Unlock()
		s.disconnectLocked()
		c.String(http.StatusOK, s.statusLocked())
	})

	return r
}

func main() {
	s := Server{}
	gin.SetMode(gin.DebugMode)
	r := setupRouter(&s)

	listenAddr := os.Getenv("GP_SAML_CLIENT_REMOTE_SERVER_LISTEN")
	if listenAddr != "" {
		log.Println("Listen on:", listenAddr)
	} else {
		log.Println("GP_SAML_CLIENT_REMOTE_SERVER_LISTEN is not configured, listen on :8080")
		listenAddr = ":8080"
	}
	srv := &http.Server{
		Addr: listenAddr,
		Handler: r,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal("Error listening:", err)
		}
	}()

	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c // Wait for signal

	ctx, cancel := context.WithTimeout(context.Background(), 5 * time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Server forced to shutdown:", err)
	}

	s.Shutdown()

	log.Println("Server exiting")
}