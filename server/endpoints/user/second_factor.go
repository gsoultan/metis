package user

import (
	"context"
	"fmt"

	"github.com/go-kit/kit/endpoint"
	"github.com/gsoultan/metis/server/domains/services"
)

// MakeSignInWithCodeEndpoint finishes a sign-in with a code from the
// authenticator app or a recovery code.
func MakeSignInWithCodeEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SecondFactorCodeRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a SecondFactorCodeRequest, got %T", request)
		}
		signIn, err := s.SignInWithCode(ctx, req.Challenge, req.Code)
		if err != nil {
			return LoginResponse{Err: err}, nil
		}
		return loginResponseOf(signIn), nil
	}
}

// MakeBeginEnrolmentEndpoint makes the secret an authenticator app is set up
// from, for the account a challenge is for.
func MakeBeginEnrolmentEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(BeginEnrolmentRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a BeginEnrolmentRequest, got %T", request)
		}
		enrolment, err := s.BeginEnrolment(ctx, req.Challenge)
		return BeginEnrolmentResponse{SecondFactorEnrolment: enrolment, Err: err}, nil
	}
}

// MakeConfirmEnrolmentEndpoint finishes setting up the authenticator app with
// its first code, and answers with the session and the recovery codes.
func MakeConfirmEnrolmentEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(SecondFactorCodeRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a SecondFactorCodeRequest, got %T", request)
		}
		signIn, codes, err := s.ConfirmEnrolment(ctx, req.Challenge, req.Code)
		if err != nil {
			return ConfirmEnrolmentResponse{LoginResponse: LoginResponse{Err: err}}, nil
		}
		return ConfirmEnrolmentResponse{LoginResponse: loginResponseOf(signIn), RecoveryCodes: codes}, nil
	}
}

// MakeSecondFactorStatusEndpoint says whether the caller signs in with an
// authenticator app. Whose comes from the session.
func MakeSecondFactorStatusEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		if _, ok := request.(SecondFactorStatusRequest); !ok {
			return nil, fmt.Errorf("user: expected a SecondFactorStatusRequest, got %T", request)
		}
		userID, err := ownAccountID(ctx)
		if err != nil {
			return SecondFactorStatusResponse{Err: err}, nil
		}
		status, err := s.SecondFactorStatus(ctx, userID)
		return SecondFactorStatusResponse{SecondFactorStatus: status, Err: err}, nil
	}
}

// MakeRegenerateRecoveryCodesEndpoint replaces the caller's recovery codes,
// given a current code from their authenticator app.
func MakeRegenerateRecoveryCodesEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(RegenerateRecoveryCodesRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a RegenerateRecoveryCodesRequest, got %T", request)
		}
		userID, err := ownAccountID(ctx)
		if err != nil {
			return RegenerateRecoveryCodesResponse{Err: err}, nil
		}
		codes, err := s.RegenerateRecoveryCodes(ctx, userID, req.Code)
		return RegenerateRecoveryCodesResponse{RecoveryCodes: codes, Err: err}, nil
	}
}

// MakeResetSecondFactorEndpoint removes the authenticator app of the account
// the path names. Who may is the gate's question and the service's, as for a
// password reset.
func MakeResetSecondFactorEndpoint(s services.ServiceFacade) endpoint.Endpoint {
	return func(ctx context.Context, request any) (any, error) {
		req, ok := request.(ResetSecondFactorRequest)
		if !ok {
			return nil, fmt.Errorf("user: expected a ResetSecondFactorRequest, got %T", request)
		}
		return ResetSecondFactorResponse{Err: s.ResetSecondFactor(ctx, req.ID)}, nil
	}
}
