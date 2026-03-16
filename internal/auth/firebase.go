package auth

import (
	"context"
	"fmt"
	"log"
	"os"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"cloud.google.com/go/firestore"
)

// FirebaseAuth wraps the Firebase Auth client and Firestore client
// for token verification and relationship lookups.
type FirebaseAuth struct {
	authClient      *auth.Client
	firestoreClient *firestore.Client
}

// NewFirebaseAuth initializes Firebase Admin SDK and returns a FirebaseAuth instance.
// It tries to use credentials from:
// 1. GOOGLE_APPLICATION_CREDENTIALS environment variable (standard).
// 2. Local "serviceAccount.json" file (convenience for development).
// 3. GCP environment default (when deployed).
func NewFirebaseAuth(ctx context.Context) (*FirebaseAuth, error) {
	var opts []option.ClientOption

	// 1. Use FIREBASE_CREDENTIALS env variable (recommended for Render)
	credJSON := os.Getenv("FIREBASE_CREDENTIALS")
	if credJSON != "" {
		log.Println("Using Firebase credentials from FIREBASE_CREDENTIALS env variable")
		opts = append(opts, option.WithCredentialsJSON([]byte(credJSON)))
	} else if _, err := os.Stat("serviceAccount.json"); err == nil {
		// 2. Fallback for local development
		log.Println("Using local serviceAccount.json for Firebase credentials")
		opts = append(opts, option.WithCredentialsFile("serviceAccount.json"))
	}

	app, err := firebase.NewApp(ctx, nil, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Firebase app: %w", err)
	}

	authClient, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Firebase Auth client: %w", err)
	}

	firestoreClient, err := app.Firestore(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Firestore client: %w", err)
	}

	log.Println("Firebase Auth and Firestore initialized successfully")
	return &FirebaseAuth{
		authClient:      authClient,
		firestoreClient: firestoreClient,
	}, nil
}

// VerifyToken verifies a Firebase ID token and returns the UID.
func (fa *FirebaseAuth) VerifyToken(ctx context.Context, idToken string) (string, error) {
	token, err := fa.authClient.VerifyIDToken(ctx, idToken)
	if err != nil {
		return "", fmt.Errorf("invalid Firebase ID token: %w", err)
	}
	return token.UID, nil
}

// RelationshipInfo holds the relationship ID and partner UID for a user.
type RelationshipInfo struct {
	RelationshipID string
	PartnerUID     string
}

// GetRelationship queries Firestore for the relationship that contains the given UID.
// It searches for the user in both "partner1Id" and "partner2Id" fields,
// matching the Firestore schema used by the iOS app.
func (fa *FirebaseAuth) GetRelationship(ctx context.Context, uid string) (*RelationshipInfo, error) {
	// Search where user is partner1.
	info, err := fa.findRelationship(ctx, "partner1Id", "partner2Id", uid)
	if err == nil {
		return info, nil
	}

	// Search where user is partner2.
	info, err = fa.findRelationship(ctx, "partner2Id", "partner1Id", uid)
	if err == nil {
		return info, nil
	}

	return nil, fmt.Errorf("no relationship found for user %s", uid)
}

func (fa *FirebaseAuth) findRelationship(ctx context.Context, selfField, partnerField, uid string) (*RelationshipInfo, error) {
	iter := fa.firestoreClient.Collection("relationships").Where(selfField, "==", uid).Documents(ctx)
	defer iter.Stop()

	doc, err := iter.Next()
	if err == iterator.Done {
		return nil, fmt.Errorf("no relationship found")
	}
	if err != nil {
		return nil, fmt.Errorf("firestore query error: %w", err)
	}

	partnerUID, ok := doc.Data()[partnerField].(string)
	if !ok {
		return nil, fmt.Errorf("partner field %s not found or invalid", partnerField)
	}

	return &RelationshipInfo{
		RelationshipID: doc.Ref.ID,
		PartnerUID:     partnerUID,
	}, nil
}

// Close cleans up the Firestore client.
func (fa *FirebaseAuth) Close() error {
	if fa.firestoreClient != nil {
		return fa.firestoreClient.Close()
	}
	return nil
}
