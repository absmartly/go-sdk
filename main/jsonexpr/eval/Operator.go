package eval

type Operator interface {
	Evaluate(evaluator Evaluator, args interface{}) interface{}
}
