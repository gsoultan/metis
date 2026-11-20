package impl

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/gsoultan/metis/internal/pkg/apierr"
	"github.com/gsoultan/metis/server/domains/entities"
	"github.com/gsoultan/metis/server/domains/services/contracts"
	repocontracts "github.com/gsoultan/metis/server/repositories/contracts"
	"github.com/gsoultan/metis/server/repositories/db"
)

// Where a server collects the releases sent to it, and says how far they got.
// Both are public: the connection's token admits the call.
const (
	ReleaseCollectPath = "/api/v1/server-connections/collect"
	ReleaseReportPath  = "/api/v1/server-connections/report"
)

const (
	// releaseCollectEvery is how often a server checks in with the ones it
	// takes releases from. A release sent is taken within it.
	releaseCollectEvery = time.Minute
	// releasesPerCollection bounds the release files one check-in carries,
	// so an answer stays well inside what a server reads.
	releasesPerCollection = 4
	// maxCollectionRounds bounds the check-ins one collection makes when
	// releases were held back.
	maxCollectionRounds = 10
	// maxCollectionBytes bounds what a server reads when it checks in: the
	// release files and their live notes, and the envelope around them.
	maxCollectionBytes = releasesPerCollection*(MaxReleaseFileBytes+MaxReleaseLiveNoteBytes) + 1<<20
	// maxReportDetail is the column's size.
	maxReportDetail = 1024
	// collectTimeout bounds one check-in: a few release files at most.
	collectTimeout = 2 * time.Minute
)

// ReleaseDeliveryParts are what sending and collecting releases needs beside
// the connections: the releases themselves, and the projects they belong to,
// which two servers match by organization and project name.
type ReleaseDeliveryParts struct {
	Releases      contracts.ReleaseService
	Projects      repocontracts.ProjectRepository
	Organizations repocontracts.OrganizationRepository
}

func (p ReleaseDeliveryParts) ready() error {
	if p.Releases == nil || p.Projects == nil || p.Organizations == nil {
		return errors.New("sending releases between servers is not available in this process")
	}
	return nil
}

// collectClient checks in with a server an administrator connected to: a
// private address is allowed, no redirect is followed, as for the handshake.
func collectClient() *http.Client {
	return &http.Client{
		Timeout: collectTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func (s *serverConnectionService) SendRelease(ctx context.Context, projectID uuid.UUID, release string, connectionID uuid.UUID) (entities.ReleaseDelivery, error) {
	if err := s.deliveries.ready(); err != nil {
		return entities.ReleaseDelivery{}, err
	}
	if env, bound := db.EnvironmentFrom(ctx); bound && env != uuid.Nil {
		return entities.ReleaseDelivery{}, apierr.Invalidf("releases are sent to other servers from this server's main runtime, not from an environment's port")
	}
	c, err := s.repo.Get(ctx, connectionID)
	if err != nil {
		return entities.ReleaseDelivery{}, err
	}
	if c.Side != entities.ConnectionDownstream {
		return entities.ReleaseDelivery{}, apierr.Invalidf("%s is a server this one takes releases from; releases are sent only to a server after this one", c.Name)
	}
	rel, err := s.deliveries.Releases.GetRelease(ctx, projectID, release)
	if err != nil {
		return entities.ReleaseDelivery{}, err
	}
	// Signed now, so a release that cannot be is refused to the person
	// sending it, not to the other server a minute later.
	if _, err := s.deliveries.Releases.ExportRelease(ctx, projectID, rel.Name); err != nil {
		return entities.ReleaseDelivery{}, err
	}
	sent, err := s.repo.SendRelease(ctx, entities.ReleaseDelivery{
		ConnectionID: c.ID, ProjectID: projectID, Release: rel.Name,
		SentBy: releaseActor(ctx), SentAt: s.now(),
	})
	if err != nil {
		return entities.ReleaseDelivery{}, err
	}
	return withServer(sent, c), nil
}

func (s *serverConnectionService) ListReleaseDeliveries(ctx context.Context, projectID uuid.UUID) ([]entities.ReleaseDelivery, error) {
	sent, err := s.repo.ListDeliveries(ctx, projectID)
	if err != nil {
		return nil, err
	}
	connections, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[uuid.UUID]entities.ServerConnection, len(connections))
	for _, c := range connections {
		byID[c.ID] = c
	}
	for i, d := range sent {
		sent[i] = withServer(d, byID[d.ConnectionID])
	}
	return sent, nil
}

func withServer(d entities.ReleaseDelivery, c entities.ServerConnection) entities.ReleaseDelivery {
	d.ServerName, d.ServerPurpose, d.LastSeenAt = c.Name, c.Purpose, c.LastSeenAt
	return d
}

// downstreamBy is the server that presents this token, or a refusal that
// says nothing about why.
func (s *serverConnectionService) downstreamBy(ctx context.Context, token string) (entities.ServerConnection, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return entities.ServerConnection{}, apierr.Forbiddenf("this server gives releases only to the servers connected to it")
	}
	c, err := s.repo.GetDownstreamByTokenHash(ctx, tokenHash(token))
	if errors.Is(err, apierr.ErrNotFound) {
		return entities.ServerConnection{}, apierr.Forbiddenf("this server gives releases only to the servers connected to it")
	}
	return c, err
}

func (s *serverConnectionService) AnswerReleaseCollection(ctx context.Context, token string) (contracts.ReleaseCollection, error) {
	if err := s.deliveries.ready(); err != nil {
		return contracts.ReleaseCollection{}, err
	}
	c, err := s.downstreamBy(ctx, token)
	if err != nil {
		return contracts.ReleaseCollection{}, err
	}
	// The token admitted a server, not a person, and the releases sent to it
	// span every organization that sent one.
	ctx = entities.WithSystemContext(ctx)
	now := s.now()
	if err := s.repo.Seen(ctx, c.ID, now); err != nil {
		return contracts.ReleaseCollection{}, err
	}
	open, err := s.repo.OpenDeliveries(ctx, c.ID)
	if err != nil {
		return contracts.ReleaseCollection{}, err
	}
	answer := contracts.ReleaseCollection{Format: contracts.ReleaseCollectionFormat, Releases: []contracts.CollectedRelease{}}
	var handed []uuid.UUID
	files := 0
	for _, d := range open {
		withFile := d.State == entities.DeliveryWaiting
		if withFile && files == releasesPerCollection {
			answer.More = true
			continue
		}
		item, err := s.collectedRelease(ctx, d, withFile)
		if err != nil {
			// Gone here since it was sent: said once, and not offered again.
			if err := s.repo.ReportDelivery(ctx, c.ID, d.ID, entities.DeliveryRefused, reportDetail(err.Error()), now); err != nil {
				return contracts.ReleaseCollection{}, err
			}
			continue
		}
		if withFile {
			files++
			handed = append(handed, d.ID)
		}
		answer.Releases = append(answer.Releases, item)
	}
	if err := s.repo.MarkCollected(ctx, c.ID, handed, now); err != nil {
		return contracts.ReleaseCollection{}, err
	}
	return answer, nil
}

// collectedRelease is one release as the server checking in is handed it:
// named by organization and project, with its signed file while that server
// has not said it has it, and this server's live note once it went live here.
func (s *serverConnectionService) collectedRelease(ctx context.Context, d entities.ReleaseDelivery, withFile bool) (contracts.CollectedRelease, error) {
	project, err := s.deliveries.Projects.Get(ctx, d.ProjectID)
	if err != nil {
		return contracts.CollectedRelease{}, fmt.Errorf("the project release %s was sent from is no longer on the sending server", d.Release)
	}
	org, err := s.deliveries.Organizations.Get(ctx, uuid.UUID(project.OrganizationID))
	if err != nil {
		return contracts.CollectedRelease{}, fmt.Errorf("the organization release %s was sent from is no longer on the sending server", d.Release)
	}
	rel, err := s.deliveries.Releases.GetRelease(ctx, d.ProjectID, d.Release)
	if err != nil {
		return contracts.CollectedRelease{}, fmt.Errorf("release %s is no longer on the sending server", d.Release)
	}
	item := contracts.CollectedRelease{ID: d.ID, Organization: org.Name, Project: project.Name, Release: rel.Name}
	if withFile {
		if item.File, err = s.deliveries.Releases.ExportRelease(ctx, d.ProjectID, rel.Name); err != nil {
			return contracts.CollectedRelease{}, fmt.Errorf("release %s could not be signed on the sending server: %w", d.Release, err)
		}
	}
	if rel.LiveAt != nil {
		note, err := s.deliveries.Releases.ReleaseLiveNote(ctx, d.ProjectID, rel.Name)
		if err != nil {
			return contracts.CollectedRelease{}, fmt.Errorf("release %s's live note could not be signed on the sending server: %w", d.Release, err)
		}
		item.LiveNote = note
	}
	return item, nil
}

func (s *serverConnectionService) AnswerReleaseReport(ctx context.Context, token string, report contracts.ReleaseReport) error {
	if report.Format != contracts.ReleaseCollectionFormat {
		return apierr.Invalidf("this server reads reports as %s", contracts.ReleaseCollectionFormat)
	}
	c, err := s.downstreamBy(ctx, token)
	if err != nil {
		return err
	}
	ctx = entities.WithSystemContext(ctx)
	now := s.now()
	if len(report.Reports) > maxReleaseReports {
		return apierr.Invalidf("a report covers at most %d releases", maxReleaseReports)
	}
	for _, r := range report.Reports {
		// Waiting is this server's word, not the other's.
		if !r.State.Valid() || r.State == entities.DeliveryWaiting {
			return apierr.Invalidf("%q is not how far a release can get", r.State)
		}
		if err := s.repo.ReportDelivery(ctx, c.ID, r.ID, r.State, reportDetail(r.Detail), now); err != nil {
			return err
		}
	}
	return s.repo.Seen(ctx, c.ID, now)
}

// maxReleaseReports bounds one report: what one check-in can hand over.
const maxReleaseReports = 200

func reportDetail(detail string) string {
	detail = strings.Map(func(r rune) rune {
		if isControlRune(r) {
			return ' '
		}
		return r
	}, strings.TrimSpace(detail))
	if len(detail) > maxReportDetail {
		cut := maxReportDetail
		for cut > 0 && !utf8RuneStart(detail[cut]) {
			cut--
		}
		detail = detail[:cut]
	}
	return detail
}

func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

func (s *serverConnectionService) CollectReleases(ctx context.Context, connectionID uuid.UUID) (contracts.CollectedReleases, error) {
	if _, err := connectionAdmin(ctx); err != nil {
		return contracts.CollectedReleases{}, err
	}
	return s.collect(entities.WithSystemContext(ctx), connectionID)
}

// collect checks in with one upstream server, takes what it hands over and
// says how far each release got here.
func (s *serverConnectionService) collect(ctx context.Context, connectionID uuid.UUID) (contracts.CollectedReleases, error) {
	if err := s.deliveries.ready(); err != nil {
		return contracts.CollectedReleases{}, err
	}
	c, err := s.repo.Get(ctx, connectionID)
	if err != nil {
		return contracts.CollectedReleases{}, err
	}
	switch {
	case c.Side != entities.ConnectionUpstream:
		return contracts.CollectedReleases{}, apierr.Invalidf("%s takes releases from this server; it collects them itself", c.Name)
	case c.AcceptedAt == nil:
		return contracts.CollectedReleases{}, apierr.Invalidf("%s is not accepted yet; a second administrator accepts it first", c.Name)
	case !c.Trusted:
		return contracts.CollectedReleases{}, apierr.Forbiddenf("%s's acceptance was not made by this server, so its releases are not taken", c.Name)
	case c.URL == "" || c.Token == "":
		return contracts.CollectedReleases{}, apierr.Invalidf("%s has no address to collect releases from", c.Name)
	}
	result := contracts.CollectedReleases{Releases: []contracts.CollectedReleaseResult{}}
	for range maxCollectionRounds {
		var handed contracts.ReleaseCollection
		if err := s.call(ctx, c, ReleaseCollectPath, struct {
			Format string `json:"format"`
		}{contracts.ReleaseCollectionFormat}, &handed, maxCollectionBytes); err != nil {
			return contracts.CollectedReleases{}, err
		}
		if handed.Format != contracts.ReleaseCollectionFormat {
			return contracts.CollectedReleases{}, apierr.Invalidf("%s did not answer as a Metis server does", c.URL)
		}
		report := contracts.ReleaseReport{Format: contracts.ReleaseCollectionFormat, Reports: []contracts.DeliveryReport{}}
		for _, item := range handed.Releases {
			state, detail := s.take(ctx, c, item)
			if len(item.File) > 0 {
				log.Info().Str("server", c.Name).Str("organization", item.Organization).Str("project", item.Project).
					Str("release", item.Release).Str("state", string(state)).Str("detail", detail).
					Msg("Collected a release")
			}
			report.Reports = append(report.Reports, contracts.DeliveryReport{ID: item.ID, State: state, Detail: detail})
			result.Releases = append(result.Releases, contracts.CollectedReleaseResult{
				Organization: item.Organization, Project: item.Project, Release: item.Release, State: state, Detail: detail,
			})
		}
		if len(report.Reports) > 0 {
			if err := s.call(ctx, c, ReleaseReportPath, report, nil, maxHandshakeReplyBytes); err != nil {
				return contracts.CollectedReleases{}, err
			}
		}
		if !handed.More {
			break
		}
	}
	if err := s.repo.Seen(ctx, c.ID, s.now()); err != nil {
		return contracts.CollectedReleases{}, err
	}
	if c, err = s.repo.Get(ctx, c.ID); err != nil {
		return contracts.CollectedReleases{}, err
	}
	c.Token = ""
	result.Connection = c
	return result, nil
}

// take keeps one release handed over, staged, and says how far it has got
// here. Every refusal is said to the sending server in this server's words.
func (s *serverConnectionService) take(ctx context.Context, c entities.ServerConnection, item contracts.CollectedRelease) (entities.DeliveryState, string) {
	projectID, err := s.projectNamed(ctx, item.Organization, item.Project)
	if err != nil {
		return entities.DeliveryRefused, err.Error()
	}
	// Who imported it, as the release's history and its approval see it: the
	// server it came from, which is nobody who can approve it.
	ctx = withReleaseActor(ctx, "server "+c.Name)
	var rel entities.Release
	if len(item.File) > 0 {
		var signed struct {
			Key string `json:"key"`
		}
		if err := json.Unmarshal(item.File, &signed); err != nil || signed.Key != c.Key {
			return entities.DeliveryRefused, fmt.Sprintf("release %s was not signed by %s's key", item.Release, c.Name)
		}
		if rel, err = s.deliveries.Releases.ImportReleaseFile(ctx, projectID, item.File); err != nil {
			// Another worker may have taken the same release a moment ago.
			again, readErr := s.deliveries.Releases.GetRelease(ctx, projectID, item.Release)
			if readErr != nil || again.Origin != entities.ReleaseOriginImported {
				return entities.DeliveryRefused, err.Error()
			}
			rel = again
		}
	} else if rel, err = s.deliveries.Releases.GetRelease(ctx, projectID, item.Release); err != nil {
		return entities.DeliveryRefused, fmt.Sprintf("release %s is no longer on this server", item.Release)
	}
	detail := ""
	if len(item.LiveNote) > 0 && rel.State == entities.ReleaseStaged && rel.LiveNote == nil {
		if noted, err := s.deliveries.Releases.AttachReleaseLiveNote(ctx, projectID, rel.Name, item.LiveNote); err != nil {
			detail = "its live note was not attached: " + err.Error()
		} else {
			rel = noted
		}
	}
	return deliveryState(rel), reportDetail(detail)
}

// projectNamed is the project here with these organization and project names:
// two servers name the same project alike, and nothing else of theirs is
// shared.
func (s *serverConnectionService) projectNamed(ctx context.Context, organization, project string) (uuid.UUID, error) {
	orgs, err := s.deliveries.Organizations.List(ctx)
	if err != nil {
		return uuid.Nil, fmt.Errorf("this server could not read its organizations: %w", err)
	}
	for _, org := range orgs {
		if org.Name != organization {
			continue
		}
		projects, err := s.deliveries.Projects.ListByOrganization(ctx, uuid.UUID(org.ID))
		if err != nil {
			return uuid.Nil, fmt.Errorf("this server could not read %s's projects: %w", organization, err)
		}
		for _, p := range projects {
			if p.Name == project {
				return uuid.UUID(p.ID), nil
			}
		}
	}
	return uuid.Nil, fmt.Errorf("this server has no project %q in an organization %q; create it, then send the release again", project, organization)
}

func deliveryState(rel entities.Release) entities.DeliveryState {
	switch rel.State {
	case entities.ReleaseLive:
		return entities.DeliveryLive
	case entities.ReleaseSuperseded:
		return entities.DeliverySuperseded
	}
	if rel.ApprovedBy != "" {
		return entities.DeliveryApproved
	}
	return entities.DeliveryStaged
}

// call posts body to the upstream server at path with the connection's token
// and reads its answer into into, refusing more than limit bytes.
func (s *serverConnectionService) call(ctx context.Context, c entities.ServerConnection, path string, body, into any, limit int64) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+path, bytes.NewReader(raw))
	if err != nil {
		return apierr.Invalidf("%s cannot be called: %v", c.URL, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)
	resp, err := s.collector.Do(req)
	if err != nil {
		return apierr.Invalidf("could not reach %s: %v", c.URL, err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Debug().Err(err).Str("url", c.URL).Msg("could not close the other server's reply")
		}
	}()
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return apierr.Invalidf("%s did not answer in full: %v", c.URL, err)
	}
	if int64(len(data)) > limit {
		return apierr.Invalidf("%s answered with more than a check-in carries", c.URL)
	}
	if resp.StatusCode != http.StatusOK {
		var refused struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &refused) == nil && refused.Error != "" {
			return apierr.Invalidf("%s refused: %s", c.URL, refused.Error)
		}
		return apierr.Invalidf("%s refused (%s)", c.URL, resp.Status)
	}
	if into == nil {
		return nil
	}
	if err := json.Unmarshal(data, into); err != nil {
		return apierr.Invalidf("%s did not answer as a Metis server does", c.URL)
	}
	return nil
}

func (s *serverConnectionService) StartReleaseCollection(ctx context.Context) {
	if s.deliveries.ready() != nil {
		return
	}
	ctx = entities.WithSystemContext(ctx)
	go func() {
		ticker := time.NewTicker(releaseCollectEvery)
		defer ticker.Stop()
		said := map[uuid.UUID]string{}
		for {
			s.collectAll(ctx, said)
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// collectAll checks in with every accepted server this one takes releases
// from. A failure is logged when it starts and when it changes, not every
// minute it lasts, and its end is logged too.
func (s *serverConnectionService) collectAll(ctx context.Context, said map[uuid.UUID]string) {
	connections, err := s.repo.List(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("Could not read the servers this one takes releases from")
		return
	}
	for _, c := range connections {
		if c.Side != entities.ConnectionUpstream || c.AcceptedAt == nil || !c.Trusted || c.URL == "" {
			continue
		}
		_, err := s.collect(ctx, c.ID)
		switch {
		case err != nil && said[c.ID] != err.Error():
			said[c.ID] = err.Error()
			log.Warn().Err(err).Str("server", c.Name).Msg("Could not collect releases; trying again every minute")
		case err == nil && said[c.ID] != "":
			delete(said, c.ID)
			log.Info().Str("server", c.Name).Msg("Collecting releases works again")
		}
	}
}

// releaseActorKey carries who a release step is done for when no person is
// signed in: the server a release was collected from.
type releaseActorKey struct{}

func withReleaseActor(ctx context.Context, actor string) context.Context {
	return context.WithValue(ctx, releaseActorKey{}, actor)
}
