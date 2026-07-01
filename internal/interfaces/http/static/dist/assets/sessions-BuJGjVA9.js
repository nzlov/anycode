import{a as e}from"./workbench-DdMeHohA.js";import{n as t}from"./graphqlClient-BTPTK_0M.js";var n=`
  id
  projectId
  projectName
  requirement
  requirementSummary
  mode
  status
  baseBranch
  currentNodeTitle
  pendingQuestion
  lastRunAt
  createdAt
  updatedAt
`,r=`
  id
  projectId
  requirement
  mode
  status
  baseBranch
  config {
    codexModel
    reasoningEffort
    permissionMode
  }
  availableActions
  canResume
  lastRunAt
  createdAt
  updatedAt
`,i=`
  id
  projectId
  requirement
  mode
  status
  baseBranch
  config {
    codexModel
    reasoningEffort
    permissionMode
  }
  lastRunAt
  createdAt
  updatedAt
`;async function a(e={}){try{let r=await t({query:`
        query Sessions($input: ListSessionsInput) {
          sessions(input: $input) {
            items {
              ${n}
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
        }
      `,variables:{input:e}});return{items:r.sessions.items.map(l),pageInfo:r.sessions.pageInfo}}catch{return f(e)}}async function o(e){let[n,i]=await Promise.all([t({query:`
        query Session($id: ID!) {
          session(id: $id) {
            ${r}
          }
        }
      `,variables:{id:e}}),t({query:`
        query SessionEvents($input: ListSessionEventsInput!) {
          sessionEvents(input: $input) {
            items {
              id
              type
              payload
              createdAt
            }
            pageInfo {
              page
              pageSize
              total
              nextCursor
            }
          }
        }
      `,variables:{input:{sessionId:e,page:1,pageSize:50}}})]);return{session:u(n.session),events:i.sessionEvents.items.map(d)}}async function s(e,n){return t({query:`
      mutation AppendPrompt($input: AppendPromptInput!) {
        appendPrompt(input: $input) {
          id
          sessionId
          body
          createdAt
        }
      }
    `,variables:{input:{sessionId:e,body:n}}})}async function c(e){return t({query:`
      mutation StopSession($id: ID!) {
        stopSession(id: $id) {
          ${i}
        }
      }
    `,variables:{id:e}})}function l(e){return{id:e.id,projectId:e.projectId,title:e.requirementSummary||y(e.requirement),summary:e.requirementSummary||e.requirement,mode:p(e.mode),status:m(e.status),branch:e.baseBranch||`main`,node:e.currentNodeTitle||_(m(e.status)),updatedAt:b(e.lastRunAt??e.updatedAt),pendingQuestion:e.pendingQuestion,filesChanged:0}}function u(e){let t=m(e.status);return{id:e.id,projectId:e.projectId,title:y(e.requirement),summary:e.requirement,mode:p(e.mode),status:t,branch:e.baseBranch||`main`,node:_(t),updatedAt:b(e.lastRunAt??e.updatedAt),pendingQuestion:t===`waiting_user`,filesChanged:0,config:e.config,availableActions:e.availableActions,canResume:e.canResume}}function d(e){return{id:e.id,kind:h(e.type),title:v(e.payload,`title`)||g(e.type),body:v(e.payload,`body`)||v(e.payload,`message`)||JSON.stringify(e.payload),time:x(e.createdAt)}}function f(t){let n=t.page??1,r=t.pageSize??e.length,i=(n-1)*r;return{items:e.slice(i,i+r),pageInfo:{page:n,pageSize:r,total:e.length,nextCursor:i+r<e.length?String(n+1):``}}}function p(e){return e===`chat`?`chat`:`workflow`}function m(e){return new Set([`created`,`starting`,`running`,`waiting_user`,`stopping`,`stopped`,`resume_failed`,`failed`,`blocked`,`completed`,`closed`]).has(e)?e:`stopped`}function h(e){return e.includes(`tool`)?`tool`:e.includes(`assistant`)?`assistant`:e.includes(`question`)?`question`:e.includes(`thought`)?`thought`:`status`}function g(e){return e.includes(`tool`)?`工具调用`:e.includes(`assistant`)?`模型输出`:e.includes(`question`)?`待回答`:e.includes(`thought`)?`思考`:`状态`}function _(e){return{created:`待运行`,starting:`启动中`,running:`运行中`,waiting_user:`待回答`,stopping:`停止中`,stopped:`已停止`,resume_failed:`恢复失败`,failed:`失败`,blocked:`阻塞`,completed:`已完成`,closed:`已关闭`}[e]}function v(e,t){let n=e[t];return typeof n==`string`?n:``}function y(e){return e.split(`
`).find(e=>e.trim())?.trim()||`未命名会话`}function b(e){return e?new Intl.DateTimeFormat(`zh-CN`,{month:`2-digit`,day:`2-digit`,hour:`2-digit`,minute:`2-digit`}).format(new Date(e)):``}function x(e){return e?new Intl.DateTimeFormat(`zh-CN`,{hour:`2-digit`,minute:`2-digit`}).format(new Date(e)):``}export{c as i,o as n,a as r,s as t};