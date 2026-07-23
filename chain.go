package main

// enum
type nodeColor int

const (
	YellowNode nodeColor = iota // 0
	GreenNode
)

type chatChain struct {
	// data
	dialogContent  *chatlist
	dialogAbstract string

	// structure
	dialogMain *chatChain
	dialogSide *chatChain
	dialogPre  *chatChain

	branchColor nodeColor
	isForkdNode bool
}

// todo
func (chain *chatChain) initChatChain() {

}

// todo
func (chain *chatChain) AppendNode() {

}

// walkthrough
func (chain *chatChain) TraverseChian() {

}

// todo
func (chain *chatChain) DeleteNode() {

}

// todo
func (chain *chatChain) UpdateNode() {

}

// todo
/* SaveChainToFile
   LoadChainFromFile */
