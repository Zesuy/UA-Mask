package proxy

import (
	"fmt"
	"time"

	"UAmask/internal/ingress"
	"UAmask/internal/model"
	"UAmask/internal/session"

	"github.com/sirupsen/logrus"
)

type Server struct {
	poolSize int
	ingress  ingress.Ingress
	manager  session.Manager
}

func NewServer(poolSize int, ingress ingress.Ingress, manager session.Manager) *Server {
	return &Server{
		poolSize: poolSize,
		ingress:  ingress,
		manager:  manager,
	}
}

func (s *Server) Run() error {
	if s.ingress == nil || s.manager == nil {
		return fmt.Errorf("server dependencies are not configured")
	}
	defer s.ingress.Close()

	logrus.Infof("UA-Mask ingress server ready")
	if s.poolSize > 0 {
		return s.runWithPool()
	}
	return s.runDefault()
}

func (s *Server) runWithPool() error {
	logrus.Infof("Starting in Worker Pool Mode (size: %d)", s.poolSize)

	sessionChan := make(chan *acceptedEnvelope, s.poolSize)
	for i := 0; i < s.poolSize; i++ {
		go func(workerID int) {
			for accepted := range sessionChan {
				logrus.Debugf("[server] Worker %d processing connection for %s", workerID, accepted.targetAddress)
				s.manager.Handle(accepted.session)
			}
		}(i)
	}

	for {
		accepted, err := s.ingress.Accept()
		if err != nil {
			logrus.Warnf("Accept error: %v; retrying...", err)
			time.Sleep(5 * time.Millisecond)
			continue
		}
		sessionChan <- &acceptedEnvelope{
			session:       accepted,
			targetAddress: accepted.Target.Address,
		}
	}
}

func (s *Server) runDefault() error {
	logrus.Info("Starting in Default Mode (one goroutine per connection)")
	for {
		accepted, err := s.ingress.Accept()
		if err != nil {
			logrus.Warnf("Accept error: %v; retrying...", err)
			time.Sleep(5 * time.Millisecond)
			continue
		}
		go s.manager.Handle(accepted)
	}
}

type acceptedEnvelope struct {
	session       *model.AcceptedSession
	targetAddress string
}
