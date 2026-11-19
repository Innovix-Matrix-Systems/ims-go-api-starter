package config

import "time"

// Queue and scheduler settings (QUEUE_*, SCHEDULE_*).

// Channel holds per-queue retry/timeout settings.
type Channel struct {
	Tries   int
	Timeout time.Duration
}

// Queue holds the queue engine settings.
type Queue struct {
	Driver            string
	WorkerConcurrency int
	Critical          Channel
	Default           Channel
	Heavy             Channel
}

// Schedule holds scheduler settings.
type Schedule struct {
	Timezone string
}

func (c *Config) loadQueue() error {
	concurrency, err := getInt("QUEUE_WORKER_CONCURRENCY", 10)
	if err != nil {
		return err
	}
	critical, err := loadChannel("QUEUE_CRITICAL_", 3, 60)
	if err != nil {
		return err
	}
	defaultChannel, err := loadChannel("QUEUE_DEFAULT_", 5, 120)
	if err != nil {
		return err
	}
	heavy, err := loadChannel("QUEUE_HEAVY_", 1, 1800)
	if err != nil {
		return err
	}
	c.Queue = Queue{
		Driver:            getString("QUEUE_DRIVER", "river"),
		WorkerConcurrency: concurrency,
		Critical:          critical,
		Default:           defaultChannel,
		Heavy:             heavy,
	}
	return nil
}

func loadChannel(prefix string, defaultTries, defaultTimeoutSeconds int) (Channel, error) {
	tries, err := getInt(prefix+"TRIES", defaultTries)
	if err != nil {
		return Channel{}, err
	}
	timeoutSeconds, err := getInt(prefix+"TIMEOUT", defaultTimeoutSeconds)
	if err != nil {
		return Channel{}, err
	}
	return Channel{Tries: tries, Timeout: time.Duration(timeoutSeconds) * time.Second}, nil
}

func (c *Config) loadSchedule() error {
	c.Schedule = Schedule{Timezone: getString("SCHEDULE_TIMEZONE", c.App.Timezone)}
	return nil
}
