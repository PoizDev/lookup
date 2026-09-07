package setup

type OutputChoice struct {
	ID, Title, Description string
	Recommended            bool
}

var outputChoices = []OutputChoice{
	{ID: "markdown", Title: "Markdown", Description: "Full analysis report in ./lookup-report.md", Recommended: true},
	{ID: "terminal", Title: "Terminal", Description: "Print the full report directly in the terminal"},
	{ID: "json", Title: "JSON", Description: "Machine-readable output for automation"},
}
