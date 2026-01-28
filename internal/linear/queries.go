package linear

// GraphQL queries and mutations.
const (
	queryViewer = `query Viewer {
  viewer { id name email }
}`

	queryMyIssues = `query MyIssues($first: Int!, $after: String, $filter: IssueFilter) {
  issues(first: $first, after: $after, filter: $filter, orderBy: updatedAt) {
    nodes {
      id
      identifier
      title
      description
      url
      priority
      state { id name type }
      team { id key name }
      project { id name }
      assignee { id name }
      labels { nodes { id name color } }
      updatedAt
      createdAt
    }
    pageInfo { hasNextPage endCursor }
  }
}`

	queryTeamStates = `query TeamStates($teamId: String!) {
  team(id: $teamId) {
    id
    name
    states { nodes { id name type } }
  }
}`

	queryIssueComments = `query IssueComments($id: String!, $first: Int!) {
  issue(id: $id) {
    id
    comments(first: $first) {
      nodes {
        id
        body
        createdAt
        user { id name }
      }
    }
  }
}`

	mutationIssueUpdate = `mutation IssueUpdate($id: String!, $stateId: String!) {
  issueUpdate(id: $id, input: { stateId: $stateId }) {
    success
    issue { id state { id name type } }
  }
}`

	mutationIssueEdit = `mutation IssueEdit($id: String!, $title: String!, $description: String) {
  issueUpdate(id: $id, input: { title: $title, description: $description }) {
    success
    issue { id title description }
  }
}`

	mutationCommentCreate = `mutation CommentCreate($issueId: String!, $body: String!) {
  commentCreate(input: { issueId: $issueId, body: $body }) {
    success
    comment { id createdAt }
  }
}`

	mutationIssueCreate = `mutation IssueCreate($teamId: String!, $title: String!, $description: String) {
  issueCreate(input: { teamId: $teamId, title: $title, description: $description }) {
    success
    issue { id identifier title url }
  }
}`
)
