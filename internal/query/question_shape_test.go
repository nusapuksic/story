package query

import "testing"

func TestIsEndingQuestion(t *testing.T) {
	for _, question := range []string{
		"How does the story end? Is it complete?",
		"What happens in the final scene?",
		"Does the ending resolve Mara's arc?",
		"Is there an epilogue?",
	} {
		if !isEndingQuestion(question) {
			t.Fatalf("isEndingQuestion(%q) = false, want true", question)
		}
	}
}

func TestIsEndingQuestionAvoidsBroadCompleteFalsePositive(t *testing.T) {
	for _, question := range []string{
		"Give me a complete list of characters.",
		"What happens at the end of August?",
		"Is the chapter complete enough to compile?",
	} {
		if isEndingQuestion(question) {
			t.Fatalf("isEndingQuestion(%q) = true, want false", question)
		}
	}
}
