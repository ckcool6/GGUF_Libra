package main

type chatChain struct {
	// structure
	dialogIndex  int
	dialogMain   *chatChain
	dialogFork   *chatChain
	dialogParent *chatChain

	// data
	dialogContent  *chatlist
	dialogAbstract string

	// node states
	isBlueNode  bool
	isGreenNode bool
}

// todo
func (chain *chatChain) initChatChain() {

}

// todo
func (chain *chatChain) AppendNode() {

}

// todo
func (chain *chatChain) DeleteNode() {

}

// todo
func (chain *chatChain) GetNodeByIndex() {

}

// todo
func (chain *chatChain) UpdateNode() {

}

//

// todo
/* SaveChainToFile
   LoadChainFromFile */
