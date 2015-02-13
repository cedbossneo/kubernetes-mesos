package scheduler

import (
	"container/ring"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/registry/etcd"
	log "github.com/golang/glog"
	mesos "github.com/mesos/mesos-go/mesosproto"
)

/**
HACK(jdef): we're not using etcd but k8s has implemented namespace support and
we're going to try to honor that by namespacing pod keys. Hence, the following
funcs that were stolen from:
    https://github.com/GoogleCloudPlatform/kubernetes/blob/release-0.5/pkg/registry/etcd/etcd.go
**/

const (
	PodPath                  = "/pods"
	NetContainerID           = "net" // container the defines the network and ipc namespaces for a pod
	defaultFinishedTasksSize = 1024
)

// makePodKey constructs etcd paths to pod items enforcing namespace rules.
func makePodKey(ctx api.Context, id string) (string, error) {
	return etcd.MakeEtcdItemKey(ctx, PodPath, id)
}

type TaskRegistry interface {
	register(*PodTask, error) (*PodTask, error)
	unregister(*PodTask)
	get(taskId string) (task *PodTask, currentState StateType)
	taskForPod(podID string) (taskID string, ok bool)
	updateStatus(status *mesos.TaskStatus) (*PodTask, StateType)
	list(filter *StateType) []string
}

type inMemoryTaskRegistry struct {
	rw            sync.RWMutex
	taskRegistry  map[string]*PodTask
	tasksFinished *ring.Ring
	podToTask     map[string]string
}

func NewInMemoryTaskRegistry() TaskRegistry {
	return &inMemoryTaskRegistry{
		taskRegistry:  make(map[string]*PodTask),
		tasksFinished: ring.New(defaultFinishedTasksSize),
		podToTask:     make(map[string]string),
	}
}

func (k *inMemoryTaskRegistry) list(filter *StateType) (taskids []string) {
	k.rw.RLock()
	defer k.rw.RUnlock()
	for id, task := range k.taskRegistry {
		if filter == nil || *filter == task.State {
			taskids = append(taskids, id)
		}
	}
	return
}

func (k *inMemoryTaskRegistry) taskForPod(podID string) (taskID string, ok bool) {
	k.rw.RLock()
	defer k.rw.RUnlock()
	// assume caller is holding scheduler lock
	taskID, ok = k.podToTask[podID]
	return
}

// registers a pod task unless the spec'd error is not nil
func (k *inMemoryTaskRegistry) register(task *PodTask, err error) (*PodTask, error) {
	if err == nil {
		k.rw.Lock()
		defer k.rw.Unlock()
		k.podToTask[task.podKey] = task.ID
		k.taskRegistry[task.ID] = task
	}
	return task, err
}

func (k *inMemoryTaskRegistry) unregister(task *PodTask) {
	k.rw.Lock()
	defer k.rw.Unlock()
	delete(k.podToTask, task.podKey)
	delete(k.taskRegistry, task.ID)
}

func (k *inMemoryTaskRegistry) get(taskId string) (*PodTask, StateType) {
	k.rw.RLock()
	defer k.rw.RUnlock()
	return k._get(taskId)
}

// assume that the caller has already locked around access to task state
func (k *inMemoryTaskRegistry) _get(taskId string) (*PodTask, StateType) {
	if task, found := k.taskRegistry[taskId]; found {
		return task, task.State
	}
	return nil, StateUnknown
}

func (k *inMemoryTaskRegistry) updateStatus(status *mesos.TaskStatus) (*PodTask, StateType) {
	taskId := status.GetTaskId().GetValue()

	k.rw.Lock()
	defer k.rw.Unlock()
	task, state := k._get(taskId)

	switch status.GetState() {
	case mesos.TaskState_TASK_STAGING:
		k.handleTaskStaging(task, state, status)
	case mesos.TaskState_TASK_STARTING:
		k.handleTaskStarting(task, state, status)
	case mesos.TaskState_TASK_RUNNING:
		k.handleTaskRunning(task, state, status)
	case mesos.TaskState_TASK_FINISHED:
		k.handleTaskFinished(task, state, status)
	case mesos.TaskState_TASK_FAILED:
		k.handleTaskFailed(task, state, status)
	case mesos.TaskState_TASK_KILLED:
		k.handleTaskKilled(task, state, status)
	case mesos.TaskState_TASK_LOST:
		k.handleTaskLost(task, state, status)
	default:
		log.Warning("unhandled task status update: %+v", status)
	}
	return task, state
}

func (k *inMemoryTaskRegistry) handleTaskStaging(task *PodTask, state StateType, status *mesos.TaskStatus) {
	log.Errorf("Not implemented: task staging")
}

func (k *inMemoryTaskRegistry) handleTaskStarting(task *PodTask, state StateType, status *mesos.TaskStatus) {
	// we expect to receive this when a launched task is finally "bound"
	// via the API server. however, there's nothing specific for us to do
	// here.
	switch state {
	case StatePending:
		task.UpdatedTime = time.Now()
		//TODO(jdef) properly emit metric, or event type instead of just logging
		task.bindTime = task.UpdatedTime
		log.V(1).Infof("metric time_to_bind %v task %v pod %v", task.bindTime.Sub(task.launchTime), task.ID, task.Pod.Name)
	default:
		log.Warningf("Ignore status TASK_STARTING because the the task is not pending")
	}
}

func (k *inMemoryTaskRegistry) handleTaskRunning(task *PodTask, state StateType, status *mesos.TaskStatus) {
	switch state {
	case StatePending:
		task.UpdatedTime = time.Now()
		log.Infof("Received running status for pending task: %+v", status)
		fillRunningPodInfo(task, status)
		task.State = StateRunning
	case StateRunning:
		task.UpdatedTime = time.Now()
		log.V(2).Info("Ignore status TASK_RUNNING because the the task is already running")
	case StateFinished:
		log.Warningf("Ignore status TASK_RUNNING because the the task is already finished")
	default:
		log.Warningf("Ignore status TASK_RUNNING (%s) because the the task is discarded", task.ID)
	}
}

func fillRunningPodInfo(task *PodTask, taskStatus *mesos.TaskStatus) {
	task.Pod.Status.Phase = api.PodRunning
	if taskStatus.Data != nil {
		var info api.PodInfo
		err := json.Unmarshal(taskStatus.Data, &info)
		if err == nil {
			task.Pod.Status.Info = info
			/// TODO(jdef) this is problematic using default Docker networking on a default
			/// Docker bridge -- meaning that pod IP's are not routable across the
			/// k8s-mesos cluster. For now, I've duplicated logic from k8s fillPodInfo
			netContainerInfo, ok := info[NetContainerID] // docker.Container
			if ok {
				if netContainerInfo.PodIP != "" {
					task.Pod.Status.PodIP = netContainerInfo.PodIP
				} else {
					log.Warningf("No network settings: %#v", netContainerInfo)
				}
			} else {
				log.Warningf("Couldn't find network container for %s in %v", task.podKey, info)
			}
		} else {
			log.Errorf("Invalid TaskStatus.Data for task '%v': %v", task.ID, err)
		}
	} else {
		log.Errorf("Missing TaskStatus.Data for task '%v'", task.ID)
	}
}

func (k *inMemoryTaskRegistry) handleTaskFinished(task *PodTask, state StateType, status *mesos.TaskStatus) {
	switch state {
	case StatePending:
		panic("Pending task finished, this couldn't happen")
	case StateRunning:
		log.V(2).Infof("received finished status for running task: %+v", status)
		delete(k.podToTask, task.podKey)
		task.State = StateFinished
		task.UpdatedTime = time.Now()
		k.tasksFinished = k.recordFinishedTask(task.ID)
	case StateFinished:
		log.Warningf("Ignore status TASK_FINISHED because the the task is already finished")
	default:
		log.Warningf("Ignore status TASK_FINISHED because the the task is not running")
	}
}

// record that a task has finished.
// older record are expunged one at a time once the historical ring buffer is saturated.
// assumes caller is holding state lock.
func (k *inMemoryTaskRegistry) recordFinishedTask(taskId string) *ring.Ring {
	slot := k.tasksFinished.Next()
	if slot.Value != nil {
		// garbage collect older finished task from the registry
		gctaskId := slot.Value.(string)
		if gctask, found := k.taskRegistry[gctaskId]; found && gctask.State == StateFinished {
			delete(k.taskRegistry, gctaskId)
		}
	}
	slot.Value = taskId
	return slot
}

func (k *inMemoryTaskRegistry) handleTaskFailed(task *PodTask, state StateType, status *mesos.TaskStatus) {
	log.Errorf("task failed: %+v", status)
	switch state {
	case StatePending:
		delete(k.taskRegistry, task.ID)
		delete(k.podToTask, task.podKey)
	case StateRunning:
		delete(k.taskRegistry, task.ID)
		delete(k.podToTask, task.podKey)
	}
}

func (k *inMemoryTaskRegistry) handleTaskKilled(task *PodTask, state StateType, status *mesos.TaskStatus) {
	defer func() {
		msg := fmt.Sprintf("task killed: %+v, task %+v", status, task)
		if task != nil && task.Has(Deleted) {
			// we were expecting this, nothing out of the ordinary
			log.V(2).Infoln(msg)
		} else {
			log.Errorln(msg)
		}
	}()
	switch state {
	case StatePending, StateRunning:
		delete(k.taskRegistry, task.ID)
		delete(k.podToTask, task.podKey)
	}
}

func (k *inMemoryTaskRegistry) handleTaskLost(task *PodTask, state StateType, status *mesos.TaskStatus) {
	log.Warningf("task lost: %+v", status)
	switch state {
	case StateRunning, StatePending:
		delete(k.taskRegistry, task.ID)
		delete(k.podToTask, task.podKey)
	}
}
