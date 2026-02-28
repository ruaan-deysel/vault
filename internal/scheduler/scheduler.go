package scheduler

import (
	"log"
	"sync"

	"github.com/robfig/cron/v3"
	"github.com/ruaandeysel/vault/internal/db"
)

type JobRunner func(jobID int64)

type Scheduler struct {
	cron    *cron.Cron
	db      *db.DB
	runner  JobRunner
	entries map[int64]cron.EntryID
	mu      sync.Mutex
}

func New(database *db.DB, runner JobRunner) *Scheduler {
	return &Scheduler{
		cron:    cron.New(),
		db:      database,
		runner:  runner,
		entries: make(map[int64]cron.EntryID),
	}
}

func (s *Scheduler) Start() error {
	jobs, err := s.db.ListJobs()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.Enabled && job.Schedule != "" {
			s.addJob(job)
		}
	}
	s.cron.Start()
	log.Printf("Scheduler started with %d jobs", len(s.entries))
	return nil
}

func (s *Scheduler) Stop() {
	s.cron.Stop()
}

func (s *Scheduler) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove all existing entries
	for jobID, entryID := range s.entries {
		s.cron.Remove(entryID)
		delete(s.entries, jobID)
	}

	// Reload from DB
	jobs, err := s.db.ListJobs()
	if err != nil {
		return err
	}
	for _, job := range jobs {
		if job.Enabled && job.Schedule != "" {
			s.addJob(job)
		}
	}
	log.Printf("Scheduler reloaded with %d jobs", len(s.entries))
	return nil
}

func (s *Scheduler) addJob(job db.Job) {
	jobID := job.ID
	entryID, err := s.cron.AddFunc(job.Schedule, func() {
		s.runner(jobID)
	})
	if err != nil {
		log.Printf("Failed to schedule job %d (%s): %v", job.ID, job.Name, err)
		return
	}
	s.entries[job.ID] = entryID
}

func (s *Scheduler) NextRun(jobID int64) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entryID, ok := s.entries[jobID]
	if !ok {
		return "", false
	}
	entry := s.cron.Entry(entryID)
	return entry.Next.Format("2006-01-02 15:04:05"), true
}
