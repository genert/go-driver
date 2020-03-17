package test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/arangodb/go-driver"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestRevisionTree(t *testing.T) {
	if getTestMode() != testModeSingle {
		t.Skipf("Not a single")
	}
	c := createClientFromEnv(t, true)
	skipBelowVersion(c, "3.7", t)

	db := ensureDatabase(nil, c, "revision_tree", nil, t)
	col := ensureCollection(nil, db, "revision_tree", nil, t)

	var noOfDocuments int = 80000
	expectedDocuments := make([]interface{}, 0, noOfDocuments)
	for i := 0; i < noOfDocuments; i++ {
		expectedDocuments = append(expectedDocuments, UserDoc{
			Name: fmt.Sprintf("User%d", 1),
			Age:  i,
		})
	}

	_, _, err := col.CreateDocuments(context.Background(), expectedDocuments)
	require.NoErrorf(t, err, "Failed to create new documents: %s", describe(err))

	batch, err := c.Replication().CreateBatch(context.Background(), db, 123, time.Hour)
	require.NoError(t, err)
	defer batch.Delete(context.Background())

	getTree := func() (driver.RevisionTree, error) {
		timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		return c.Replication().GetRevisionTree(timeoutCtx, db, batch.BatchID(), col.Name())
	}

	tree, err := getTree()
	if err != nil {
		if err.Error() == "this collection doesn't support revision-based replication" {
			// TODO why it happens
			t.Skip("Collection " + col.Name() + "does not support revision-based replication")
		}
		require.NoError(t, err)
	}

	require.NotEmpty(t, tree.Version)
	require.NotEmpty(t, tree.RangeMin)
	require.NotEmpty(t, tree.RangeMax)
	require.NotEmpty(t, tree.Nodes)
	require.Equal(t, 6, tree.MaxDepth)

	getRanges := func() driver.Revisions {
		timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		rangeRevisions := []driver.RevisionMinMax{{tree.RangeMin, tree.RangeMax}}
		var resume driver.RevisionInt64
		revisions := make(driver.Revisions, 0)

		for {
			ranges, err := c.Replication().GetRevisionsByRanges(timeoutCtx, db, batch.BatchID(), col.Name(),
				rangeRevisions, resume)
			require.NoError(t, err)
			revisions = append(revisions, ranges.Ranges[0]...)

			if ranges.Resume == 0 {
				break
			}
			resume = ranges.Resume
		}
		return revisions
	}

	revisions := getRanges()
	require.NotEmpty(t, revisions)
	require.Len(t, revisions, noOfDocuments)

	getDocuments := func() ([]map[string]interface{}, error) {
		time.Sleep(3) // TODO  why we need to wait for documents
		timeoutCtx, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()

		return c.Replication().GetRevisionDocuments(timeoutCtx, db, batch.BatchID(), col.Name(), revisions)
	}

	documents, err := getDocuments()
	require.NoError(t, err)
	require.Len(t, documents, noOfDocuments)

	for i, d := range documents {
		user := UserDoc{}
		bytes, _ := json.Marshal(d)
		json.Unmarshal(bytes, &user)
		require.Equal(t, user, expectedDocuments[i])
	}
}
