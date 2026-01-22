package model

import damlModel "github.com/noders-team/go-daml/pkg/model"

type WrappedCommand struct {
	ExerciseCommand          *ExerciseCommand
	CreateCommand            *CreateCommand
	CreateAndExerciseCommand *CreateAndExerciseCommand
}

type CreateCommand struct {
	TemplateID      string
	CreateArguments map[string]interface{}
}

func (c *CreateCommand) ToDamlCreateCommand() *damlModel.Command {
	return &damlModel.Command{
		Command: &damlModel.CreateCommand{
			TemplateID: c.TemplateID,
			Arguments:  c.CreateArguments,
		},
	}
}

type ExerciseCommand struct {
	ContractID      string
	TemplateID      string
	Choice          string
	ChoiceArguments map[string]interface{}
}

type CreateAndExerciseCommand struct {
	TemplateID      string
	CreateArguments map[string]interface{}
	Choice          string
	ChoiceArguments map[string]interface{}
}
