package question

import (
	"errors"
	"fmt"
	"strings"
)

type DefaultPolicy struct{}

func (DefaultPolicy) CanSubmit(batch Batch, answers []Answer) error {
	if batch.ID == "" {
		return errors.New("question batch id is required")
	}
	if batch.Status != BatchPending {
		return fmt.Errorf("question batch %s is not pending", batch.ID)
	}
	questions := make(map[QuestionID]Question, len(batch.Questions))
	for _, item := range batch.Questions {
		if item.ID == "" {
			return errors.New("question id is required")
		}
		questions[item.ID] = item
	}
	if len(answers) != len(questions) {
		return errors.New("all questions must be answered")
	}
	seen := make(map[QuestionID]struct{}, len(answers))
	for _, answer := range answers {
		question, ok := questions[answer.QuestionID]
		if !ok {
			return fmt.Errorf("answer references unknown question %s", answer.QuestionID)
		}
		if _, ok := seen[answer.QuestionID]; ok {
			return fmt.Errorf("question %s has duplicate answers", answer.QuestionID)
		}
		seen[answer.QuestionID] = struct{}{}
		if err := validateAnswer(question, answer); err != nil {
			return err
		}
	}
	for id := range questions {
		if _, ok := seen[id]; !ok {
			return fmt.Errorf("question %s is missing an answer", id)
		}
	}
	return nil
}

func (p DefaultPolicy) ApplyAnswers(batch Batch, answers []Answer) (Batch, error) {
	if err := p.CanSubmit(batch, answers); err != nil {
		return Batch{}, err
	}
	byQuestion := make(map[QuestionID]Answer, len(answers))
	for _, answer := range answers {
		byQuestion[answer.QuestionID] = answer
	}
	for i := range batch.Questions {
		answer := byQuestion[batch.Questions[i].ID]
		batch.Questions[i].SelectedOptionID = answer.SelectedOptionID
		batch.Questions[i].CustomAnswer = answer.CustomAnswer
		batch.Questions[i].Answer = answer.Payload
		batch.Questions[i].Status = "answered"
	}
	batch.Status = BatchAnswered
	return batch, nil
}

func (DefaultPolicy) Cancel(batch Batch, reason string) (Batch, error) {
	if batch.Status != BatchPending {
		return Batch{}, fmt.Errorf("question batch %s is not pending", batch.ID)
	}
	batch.Status = BatchCancelled
	return batch, nil
}

func validateAnswer(question Question, answer Answer) error {
	hasOption := answer.SelectedOptionID != nil
	hasCustom := strings.TrimSpace(answer.CustomAnswer) != ""
	switch {
	case hasOption && hasCustom:
		return fmt.Errorf("question %s answer cannot use option and custom answer together", question.ID)
	case hasOption:
		for _, option := range question.Options {
			if option.ID == *answer.SelectedOptionID {
				return nil
			}
		}
		return fmt.Errorf("question %s selected option %s is invalid", question.ID, *answer.SelectedOptionID)
	case hasCustom:
		return nil
	default:
		return fmt.Errorf("question %s requires an option or custom answer", question.ID)
	}
}
