package topology

import (
	"code.google.com/p/weed-fs/go/glog"
	"code.google.com/p/weed-fs/go/sequence"
	"code.google.com/p/weed-fs/go/storage"
	"errors"
	"io/ioutil"
	"math/rand"
)

type Topology struct {
	NodeImpl

	collectionMap map[string]*Collection

	pulse int64

	volumeSizeLimit uint64

	sequence sequence.Sequencer

	chanDeadDataNodes      chan *DataNode
	chanRecoveredDataNodes chan *DataNode
	chanFullVolumes        chan storage.VolumeInfo

	configuration *Configuration
}

func NewTopology(id string, confFile string, seq sequence.Sequencer, volumeSizeLimit uint64, pulse int) (*Topology, error) {
	t := &Topology{}
	t.id = NodeId(id)
	t.nodeType = "Topology"
	t.NodeImpl.value = t
	t.children = make(map[NodeId]Node)
	t.collectionMap = make(map[string]*Collection)
	t.pulse = int64(pulse)
	t.volumeSizeLimit = volumeSizeLimit

	t.sequence = seq

	t.chanDeadDataNodes = make(chan *DataNode)
	t.chanRecoveredDataNodes = make(chan *DataNode)
	t.chanFullVolumes = make(chan storage.VolumeInfo)

	err := t.loadConfiguration(confFile)

	return t, err
}

func (t *Topology) loadConfiguration(configurationFile string) error {
	b, e := ioutil.ReadFile(configurationFile)
	if e == nil {
		t.configuration, e = NewConfiguration(b)
		return e
	} else {
		glog.V(0).Infoln("Using default configurations.")
	}
	return nil
}

func (t *Topology) Lookup(collection string, vid storage.VolumeId) []*DataNode {
	//maybe an issue if lots of collections?
	if collection == "" {
		for _, c := range t.collectionMap {
			if list := c.Lookup(vid); list != nil {
				return list
			}
		}
	} else {
		if c, ok := t.collectionMap[collection]; ok {
			return c.Lookup(vid)
		}
	}
	return nil
}

func (t *Topology) RandomlyReserveOneVolume(dataCenter string) (bool, *DataNode, *storage.VolumeId) {
	if t.FreeSpace() <= 0 {
		glog.V(0).Infoln("Topology does not have free space left!")
		return false, nil, nil
	}
	vid := t.NextVolumeId()
	ret, node := t.ReserveOneVolume(rand.Intn(t.FreeSpace()), vid, dataCenter)
	return ret, node, &vid
}

func (t *Topology) NextVolumeId() storage.VolumeId {
	vid := t.GetMaxVolumeId()
	return vid.Next()
}

func (t *Topology) PickForWrite(collectionName string, repType storage.ReplicationType, count int, dataCenter string) (string, int, *DataNode, error) {
	vid, count, datanodes, err := t.GetVolumeLayout(collectionName, repType).PickForWrite(count, dataCenter)
	if err != nil || datanodes.Length() == 0 {
		return "", 0, nil, errors.New("No writable volumes avalable!")
	}
	fileId, count := t.sequence.NextFileId(count)
	return storage.NewFileId(*vid, fileId, rand.Uint32()).String(), count, datanodes.Head(), nil
}

func (t *Topology) GetVolumeLayout(collectionName string, repType storage.ReplicationType) *VolumeLayout {
	_, ok := t.collectionMap[collectionName]
	if !ok {
		t.collectionMap[collectionName] = NewCollection(collectionName, t.volumeSizeLimit)
	}
	return t.collectionMap[collectionName].GetOrCreateVolumeLayout(repType)
}

func (t *Topology) RegisterVolumeLayout(v *storage.VolumeInfo, dn *DataNode) {
	t.GetVolumeLayout(v.Collection, v.RepType).RegisterVolume(v, dn)
}

func (t *Topology) RegisterVolumes(init bool, volumeInfos []storage.VolumeInfo, ip string, port int, publicUrl string, maxVolumeCount int, dcName string, rackName string) {
	dcName, rackName = t.configuration.Locate(ip, dcName, rackName)
	dc := t.GetOrCreateDataCenter(dcName)
	rack := dc.GetOrCreateRack(rackName)
	dn := rack.FindDataNode(ip, port)
	if init && dn != nil {
		t.UnRegisterDataNode(dn)
	}
	dn = rack.GetOrCreateDataNode(ip, port, publicUrl, maxVolumeCount)
	for _, v := range volumeInfos {
		dn.AddOrUpdateVolume(v)
		t.RegisterVolumeLayout(&v, dn)
	}
}

func (t *Topology) GetOrCreateDataCenter(dcName string) *DataCenter {
	for _, c := range t.Children() {
		dc := c.(*DataCenter)
		if string(dc.Id()) == dcName {
			return dc
		}
	}
	dc := NewDataCenter(dcName)
	t.LinkChildNode(dc)
	return dc
}
