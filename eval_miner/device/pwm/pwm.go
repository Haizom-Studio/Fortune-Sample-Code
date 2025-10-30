package pwm

import (
	"bytes"
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"
)

type PWMPin struct {
	chipPath string
	channel  string
	enabled  bool
}

func NewPin(pwmChipID int, channel int) *PWMPin {
	return &PWMPin{
		chipPath: "/sys/class/pwm/pwmchip" + strconv.Itoa(pwmChipID),
		channel:  strconv.Itoa(channel),
		enabled:  false,
	}
}

func (p *PWMPin) Export() error {
	err := os.WriteFile(p.chipPath+"/export", []byte(p.channel), 0644)
	if err != nil {
		e, ok := err.(*os.PathError)
		if !ok || e.Err != syscall.EBUSY {
			return err
		}
	}

	time.Sleep(200 * time.Millisecond)

	return nil
}

func (p *PWMPin) Unexport() error {
	return os.WriteFile(p.chipPath+"/unexport", []byte(p.channel), 0644)
}

func (p *PWMPin) pinDir() string {
	return p.chipPath + "/pwm" + p.channel
}

func (p *PWMPin) Enable(enable bool) error {
	if p.enabled != enable {
		p.enabled = enable

		if enable {
			return os.WriteFile(p.pinDir()+"/enable", []byte("1"), 0644)
		} else {
			return os.WriteFile(p.pinDir()+"/enable", []byte("0"), 0644)
		}
	}

	return nil
}

func (p *PWMPin) GetPeriod() (period uint32, err error) {
	buf, err := os.ReadFile(p.pinDir() + "/period")
	if err != nil {
		return 0, err
	}
	if len(buf) == 0 {
		return 0, nil
	}

	v := bytes.TrimRight(buf, "\n")
	val, e := strconv.Atoi(string(v))
	return uint32(val), e
}

func (p *PWMPin) SetPeriod(period uint32) error {
	return os.WriteFile(p.pinDir()+"/period", []byte(fmt.Sprintf("%v", period)), 0644)
}

func (p *PWMPin) GetDutyCycle() (duty uint32, err error) {
	buf, err := os.ReadFile(p.pinDir() + "/duty_cycle")
	if err != nil {
		return
	}

	v := bytes.TrimRight(buf, "\n")
	val, e := strconv.Atoi(string(v))
	return uint32(val), e
}

func (p *PWMPin) SetDutyCycle(duty uint32) error {
	return os.WriteFile(p.pinDir()+"/duty_cycle", []byte(fmt.Sprintf("%v", duty)), 0644)
}

func (p *PWMPin) SetDutyCyclePercent(percent uint32) error {
	period, err := p.GetPeriod()
	if err != nil {
		return err
	}
	duty := period / 100 * percent
	return os.WriteFile(p.pinDir()+"/duty_cycle", []byte(fmt.Sprintf("%v", duty)), 0644)
}

func (p *PWMPin) GetDutyCyclePercent() (uint32, error) {
	period, err := p.GetPeriod()
	if err != nil {
		return 0, err
	}
	dutyCycle, err := p.GetDutyCycle()
	if err != nil {
		return 0, err
	}

	return dutyCycle * 100 / period, nil
}
