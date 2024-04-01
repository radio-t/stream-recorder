package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
)

type Status string //	@name	HealthStatus

const (
	Pass Status = "pass"
	Fail Status = "fail"
	Warn Status = "warn"
)

// Health is response model according to https://inadarei.github.io/rfc-healthcheck/
type Health struct {
	// Status indicates whether the service status is acceptable or not.
	// - `pass`: healthy (acceptable aliases: "ok" to support Node's Terminus and "up" for Java's SpringBoot),
	// - `fail`: unhealthy (acceptable aliases: "error" to support Node's Terminus and "down" for Java's SpringBoot), and
	// - `warn`: healthy, with some concerns.
	Status Status `json:"status" example:"pass" enum:"pass,fail,warn"`

	// Version - public version of the service. (API version).
	Version string `json:"apiVersion" example:"1"`

	Revision string `json:"revision"`

	// Notes - array of notes relevant to current state of health.
	Notes []string `json:"notes" example:"commit hash: aee0773,uptime: 12.3s"`

	// Description - a human-friendly description of the service.
	Description string `json:"description"`
} //	@name	Health

type HealthController struct {
	dir string
}

func NewHealthController(dir string) *HealthController {
	return &HealthController{
		dir: dir,
	}
}

func (h *Health) HTTPStatus() int {
	switch h.Status {
	case Pass:
		return http.StatusOK
	case Fail:
		return http.StatusInternalServerError
	case Warn:
		return http.StatusOK
	default:
		return http.StatusInternalServerError
	}
}

func (h *HealthController) Check() (*Health, error) {
	warncapacity := 80
	health := &Health{} //nolint:exhaustruct

	capacity, err := h.getCapacity()
	if err != nil {
		return nil, err
	}

	if capacity > warncapacity {
		health.Status = Fail
		health.Notes = append(health.Notes, "disk is more than 80% full")
	} else {
		health.Status = Pass
	}

	health.Description = fmt.Sprintf("disk capacity: %d%%", capacity)

	return health, nil
}

// HealthHandler
//
//	@Router			 /health [get]
//	@Tags				 System
//	@Summary		 Health Check
//	@Description Provides information about the application health.
//	@Produce		 json
//	@Success		 200	{object}	Health
func (s *Server) Health(w http.ResponseWriter, r *http.Request) {
	health, err := s.HealthController.Check()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error())) //nolint:errcheck
		return
	}

	data, err := json.MarshalIndent(health, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(err.Error())) //nolint:errcheck
		return
	}

	w.WriteHeader(health.HTTPStatus())
	w.Write(data) //nolint:errcheck
}

func (h *HealthController) getCapacity() (int, error) {
	command := `df -h ` + h.dir + ` | awk 'NR==2 {gsub("%","",$5); print $5}'`
	cmd := exec.Command("sh", "-c", command)
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get capacity: %w", err)
	}

	output := string(out)
	output = strings.Trim(output, " \n")

	capacity, err := strconv.Atoi(output)
	if err != nil {
		return 0, fmt.Errorf("failed to convert capacity: %s, error: %w", output, err)
	}

	return capacity, nil
}
