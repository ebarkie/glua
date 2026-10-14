package glua

// VM is a private, mutable instance of a Script. One goroutine at a time.
type VM struct {
	s  *Script
	ex *executor // its own, kept between calls; replaced after an abnormal end
}

// enter returns the VM's executor for a call.
func (vm *VM) enter() *executor {
	if vm.ex == nil || vm.ex.poisoned {
		vm.ex = vm.s.rt.newExecutor()
	}
	vm.ex.setLimits(vm.s.lim)
	return vm.ex
}

// leave clears the executor after a call.
func (vm *VM) leave(ex *executor) {
	ex.reset()
}
