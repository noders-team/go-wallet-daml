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

	emitListeners(e.accountsListeners, wallets)
}

func (e *EventEmitter) EmitTxChanged(event interface{}) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	emitListeners(e.txListeners, event)
}

func (e *EventEmitter) AddAccountsListener(ch chan []*model.Wallet) {
	e.mu.Lock()
	defer e.mu.Unlock()
	addListener(&e.accountsListeners, ch)
}

func (e *EventEmitter) AddTxListener(ch chan interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	addListener(&e.txListeners, ch)
}

func (e *EventEmitter) RemoveAccountsListener(ch chan []*model.Wallet) {
	e.mu.Lock()
	defer e.mu.Unlock()
	removeListener(&e.accountsListeners, ch)
}

func (e *EventEmitter) RemoveTxListener(ch chan interface{}) {
	e.mu.Lock()
	defer e.mu.Unlock()
	removeListener(&e.txListeners, ch)
}

func (e *EventEmitter) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()

	closeListeners(e.accountsListeners)
	closeListeners(e.txListeners)

	e.accountsListeners = nil
	e.txListeners = nil
}

func emitListeners[T any](listeners []chan T, value T) {
	for _, ch := range listeners {
		select {
		case ch <- value:
		default:
		}
	}
}

func addListener[T any](listeners *[]chan T, ch chan T) {
	*listeners = append(*listeners, ch)
}

func removeListener[T any](listeners *[]chan T, ch chan T) {
	for i, listener := range *listeners {
		if listener == ch {
			*listeners = append((*listeners)[:i], (*listeners)[i+1:]...)
			break
		}
	}
}

func closeListeners[T any](listeners []chan T) {
	for _, ch := range listeners {
		close(ch)
	}
}
