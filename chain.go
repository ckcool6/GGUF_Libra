package main

type chatChain struct {
	dialogIndex    int
	dialogNext     *chatChain
	dialogPre      *chatChain
	dialogContent  *chatlist
	dialogAbstract string
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
