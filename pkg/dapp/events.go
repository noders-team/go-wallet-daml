package dapp

import (
	"sync"

	"github.com/noders-team/go-wallet-daml/pkg/model"
)

type EventEmitter struct {
	accountsListeners []chan []*model.Wallet
	txListeners       []chan interface{}
	mu                sync.RWMutex
}

func NewEventEmitter() *EventEmitter {
	return &EventEmitter{
		accountsListeners: make([]chan []*model.Wallet, 0),
		txListeners:       make([]chan interface{}, 0),
	}
}

func (e *EventEmitter) EmitAccountsChanged(wallets []*model.Wallet) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, ch := range e.accountsListeners {
		select {
		case ch <- wallets:
		default:
		}
	}
}

func (e *EventEmitter) EmitTxChanged(event interface{}) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for _, ch := range e.txListeners {
		select {
		case ch <- event:
		default:
		}
	}
}

func (e *EventEmitter) AddAccountsListener(ch chan []*model.Wallet) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.accountsListeners = append(e.accountsListeners, ch)
}

func (e *EventEmitter) AddTxListener(ch chan interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.txListeners = append(e.txListeners, ch)
}

func (e *EventEmitter) RemoveAccountsListener(ch chan []*model.Wallet) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, listener := range e.accountsListeners {
		if listener == ch {
			e.accountsListeners = append(e.accountsListeners[:i], e.accountsListeners[i+1:]...)
			break
		}
	}
}

func (e *EventEmitter) RemoveTxListener(ch chan interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()

	for i, listener := range e.txListeners {
		if listener == ch {
			e.txListeners = append(e.txListeners[:i], e.txListeners[i+1:]...)
			break
		}
	}
}

func (e *EventEmitter) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	for _, ch := range e.accountsListeners {
		close(ch)
	}
	for _, ch := range e.txListeners {
		close(ch)
	}

	e.accountsListeners = nil
	e.txListeners = nil
}
