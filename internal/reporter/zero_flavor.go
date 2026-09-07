package reporter

import "hash/fnv"

var approvedZeroFindingFlavors = [...]string{
	"Lookup has nothing to complain about. Rare.",
	"Suspiciously clean. Nice work.",
	"Well... this is awkward. Good job.",
	"Nothing suspicious. I checked twice.",
	"Either this codebase is clean, or you're very good at hiding things.",
	"I was promised problems. I demand an explanation.",
	"No findings. I feel slightly unemployed.",
	"This is usually where I ruin someone's afternoon. Not today.",
	"Nothing to report. That's... unusually pleasant.",
	"I looked. I judged. I found nothing.",
	"No findings. You may proceed with unreasonable confidence.",
	"Clean enough to make me suspicious.",
	"I had notes prepared. Never mind.",
	"No findings. Don't get used to this.",
	"The code survived inspection. Congratulations.",
	"Nothing caught my attention. That's a compliment.",
	"No findings. Somewhere, a linter is disappointed.",
	"I searched for trouble. Trouble declined to participate.",
	"Everything looks fine. This makes me deeply uncomfortable.",
	"No findings. Please don't make this weird.",
	"Zero findings. Task failed successfully.",
	"No findings. Fine. Keep your secrets.",
	"The codebase is innocent until proven guilty. Today, it stays innocent.",
	"I expected a crime scene. This is disappointingly well maintained.",
	"I came. I saw. I found nothing.",
	"I have investigated myself and found no wrongdoing.",
	"The findings were the friends we made along the way.",
	"Nothing to see here. I checked anyway.",
	"I searched everywhere. Apparently, you cleaned before I got here.",
	"No findings. Management will be disappointed.",
	"Everything checks out. I don't like it.",
	"No findings. Against all expectations, we're done here.",
	"I expected paperwork. You ruined everything.",
	"No findings. This meeting could have been an email.",
	"Nothing broke. Nothing leaked. Nothing offended me. Impressive.",
	"I brought a red pen for nothing.",
	"They came as findings. They left as they came.",
	"I don't have findings. I have standards.",
	"I don't play the odds. I play the evidence.",
	"No findings. Suit up anyway.",
	"Challenge accepted. Unfortunately, there was nothing to challenge.",
	"No findings. Legendary? Let's not get carried away.",
	"Best. Scan. Ever.",
	"No findings. Excellent.",
	"Everything's coming up clean code.",
	"No findings. Freakin' sweet.",
	"This is less suspicious than the time I checked that other repository.",
	"No findings. Victory is mine.",
	"No findings. Somehow, this still could have been an email.",
	"No findings. Identity theft remains a serious crime.",
	"No findings. Assistant to the regional static analyzer.",
	"No findings. Cool cool cool cool cool.",
	"Noice. Smort. Clean.",
	"Could this codebase BE any cleaner?",
	"No findings. We were not on a break.",
	"No findings. Not that there's anything wrong with that.",
	"No findings. Streets ahead.",
	"Six seasons and zero findings.",
	"No findings. Treat yo' self.",
	"No findings. Apparently turning it off and on again worked.",
	"No findings. What is this, a crossover episode?",
	"No findings. You're goddamn right.",
	"Case closed. Disturbingly quickly.",
	"The prosecution rests. Mostly because it found nothing.",
	"You rolled a natural 20 on code review.",
	"No findings. Achievement unlocked: Suspiciously Competent.",
	"It works on my machine. Apparently yours too.",
	"No findings. Ship it before something changes.",
	"No findings. Quick, merge before anyone notices.",
	"Works as intended. For once.",
	"No findings. git blame has been postponed.",
	"No findings. Production remains someone else's problem.",
	"I found nothing. This investigation remains emotionally unresolved.",
	"There are no findings in Ba Sing Se.",
	"No findings. Even the Council of Ricks found nothing.",
	"No findings. Somewhere, a Meeseeks can finally disappear.",
}

func zeroFindingFlavor(repositoryIdentity string, revision ...string) string {
	repositoryRevision := ""
	if len(revision) > 0 {
		repositoryRevision = revision[0]
	}
	identity := zeroFindingFlavorIdentity(repositoryIdentity, repositoryRevision)
	return approvedZeroFindingFlavors[zeroFindingFlavorIndex(identity)]
}

func zeroFindingFlavorIdentity(repositoryIdentity, repositoryRevision string) string {
	if repositoryRevision == "" {
		return repositoryIdentity
	}
	return repositoryIdentity + "\x00" + repositoryRevision
}

func zeroFindingFlavorIndex(identity string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(identity))
	return int(h.Sum32()) % len(approvedZeroFindingFlavors)
}
