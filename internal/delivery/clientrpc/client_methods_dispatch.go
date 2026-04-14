package clientrpc

import (
	"context"
	"fmt"
)

func (h *Handler) dispatchClientMethod(ctx context.Context, rpcCall string, params map[string]any, state sessionState) (map[string]any, error) {
	switch rpcCall {
	case "getProfile", "updateProfile", "searchProfiles", "deleteAccount", "getProfileAvatar":
		return h.handleProfileMethods(ctx, rpcCall, params, state)
	case "listDevices", "registerDevicePushToken", "removeDevice", "uploadKeyPackages", "fetchKeyPackages", "sendCommit", "sendWelcome", "uploadGroupInfo", "fetchGroupInfo", "sendExternalCommit", "fetchWelcome":
		return h.handleDeviceAndMLSMethods(ctx, rpcCall, params, state)
	case "listFriends", "removeFriend", "sendFriendRequest", "acceptFriendRequest", "declineFriendRequest", "cancelFriendRequest", "listFriendRequests":
		return h.handleFriendMethods(ctx, rpcCall, params, state)
	case "createChatRoom", "createDirectRoom", "listChatRooms", "getChatRoom", "searchChatRooms", "syncChatRoom", "updateChatRoom", "updateChatRoomState", "fetchChatRoomState", "deleteChatRoom", "getChatRoomAvatar":
		return h.handleRoomMethods(ctx, rpcCall, params, state)
	case "joinChatRoom", "leaveChatRoom", "kickChatMember", "listChatMembers", "updateChatMemberRole", "createChatMemberPermission", "listChatMemberPermissions", "updateChatMemberPermission", "deleteChatMemberPermission":
		return h.handleMemberMethods(ctx, rpcCall, params, state)
	case "sendChatInvitation", "revokeChatInvitation", "listSentChatInvitations", "listIncomingChatInvitations", "acceptChatInvitation", "declineChatInvitation":
		return h.handleInvitationMethods(ctx, rpcCall, params, state)
	case "sendMessage", "deleteMessage":
		return h.handleMessageMethods(ctx, rpcCall, params, state)
	case "getServerLimits", "getUserLimits", "getGroupLimits", "getServerConfig":
		return h.handleOverviewMethods(ctx, rpcCall, params, state)
	default:
		return nil, fmt.Errorf("rpc %s is not implemented yet", rpcCall)
	}
}
