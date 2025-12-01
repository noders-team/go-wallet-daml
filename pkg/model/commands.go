package model

type WrappedCommand struct {
	ExerciseCommand          *ExerciseCommand
	CreateCommand            *CreateCommand
	CreateAndExerciseCommand *CreateAndExerciseCommand
}

type CreateCommand struct {
	TemplateID      string
	CreateArguments map[string]interface{}
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
