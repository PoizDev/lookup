package setup

type ProviderChoice struct{ Name, ID, Description string }

var providerChoices = []ProviderChoice{{"OpenAI", "openai", "GPT models"}, {"Anthropic", "anthropic", "Claude models"}, {"Google Gemini", "gemini", "Gemini models"}, {"Ollama", "ollama", "Local models"}}
