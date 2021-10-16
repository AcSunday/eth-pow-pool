package routine

type MaxAmountCtrl struct {
	count chan struct{}
}

func NewMaxAmountCtrl(max int) *MaxAmountCtrl {
	return &MaxAmountCtrl{
		count: make(chan struct{}, max),
	}
}

func (mac *MaxAmountCtrl) Incr() {
	mac.count <- struct{}{}
}

func (mac *MaxAmountCtrl) Decr() {
	<-mac.count
}
