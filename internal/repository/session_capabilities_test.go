package repository

import "errors"

// openFullRepositorySessionForTest keeps existing integration fixtures working
// while production callers migrate to narrow capabilities. The real opener is
// required to provide every repository feature these integration tests use.
func openFullRepositorySessionForTest(session Session, err error) (FullSession, error) {
	if err != nil {
		return nil, err
	}
	full, ok := session.(FullSession)
	if !ok {
		return nil, errors.New("repository session does not provide the required integration-test capabilities")
	}
	return full, nil
}
