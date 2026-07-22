package question

import (
	"errors"
	"fmt"
	"strings"
)

type DefaultPolicy struct{}

func (DefaultPolicy) CanSubmit(request Request, answers []Answer) error {
	if request.ID == "" {
		return errors.New("question request id is required")
	}
	if request.Status != RequestPending {
		return fmt.Errorf("question request %s is not pending", request.ID)
	}
	questions := make(map[QuestionID]Question, len(request.Questions))
	for _, item := range request.Questions {
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

func (p DefaultPolicy) ApplyAnswers(request Request, answers []Answer) (Request, error) {
	if err := p.CanSubmit(request, answers); err != nil {
		return Request{}, err
	}
	byQuestion := make(map[QuestionID]Answer, len(answers))
	for _, answer := range answers {
		byQuestion[answer.QuestionID] = answer
	}
	for i := range request.Questions {
		answer := byQuestion[request.Questions[i].ID]
		request.Questions[i].SelectedOptionID = answer.SelectedOptionID
		request.Questions[i].CustomAnswer = answer.CustomAnswer
		request.Questions[i].Answer = answer.Payload
		request.Questions[i].Status = "answered"
	}
	request.Status = RequestAnswered
	return request, nil
}

func (DefaultPolicy) Cancel(request Request, reason string) (Request, error) {
	if request.Status != RequestPending {
		return Request{}, fmt.Errorf("question request %s is not pending", request.ID)
	}
	request.Status = RequestCancelled
	return request, nil
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
