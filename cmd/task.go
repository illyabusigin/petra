package main

type tasks []task

func (t *tasks) queue(tasks ...task) {
	*t = append(*t, tasks...)
}

func (t tasks) run() error {
	for _, task := range t {
		if err := task(); err != nil {
			return err
		}
	}

	return nil
}

type task func() error
